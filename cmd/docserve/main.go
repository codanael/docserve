package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codanael/docserve/internal/config"
	"github.com/codanael/docserve/internal/index"
	mcpsrv "github.com/codanael/docserve/internal/mcp"
	"github.com/codanael/docserve/internal/scheduler"
	"github.com/codanael/docserve/internal/source"
)

var version = "dev"

func printUsage() {
	fmt.Fprintf(os.Stderr, `docserve — self-hosted MCP documentation server

Usage:
  docserve <command> [arguments]

Commands:
  serve    Start the MCP documentation server
  fetch    Fetch and index documentation from a source
  list     List indexed documentation sources
  search   Search indexed documentation
  version  Print version information

Run 'docserve <command> -help' for more information on a command.
`)
}

// loadConfig parses flagArgs for a --config flag, resolves the config path, and loads it.
func loadConfig(flagArgs []string) (*config.Config, *flag.FlagSet) {
	fs := flag.NewFlagSet("", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Parse(flagArgs) //nolint:errcheck

	resolved, err := config.ResolvePath(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	return cfg, fs
}

// openStore creates the data directory if needed and opens the SQLite store.
func openStore(cfg *config.Config) *index.Store {
	dbPath := filepath.Join(cfg.DataDir, "docserve.db")
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating data dir: %v\n", err)
		os.Exit(1)
	}

	store, err := index.OpenStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening store: %v\n", err)
		os.Exit(1)
	}

	return store
}

// buildClients constructs direct and (optionally) proxied HTTP clients.
func buildClients(cfg *config.Config) (proxied, direct *http.Client) {
	direct = &http.Client{Timeout: 5 * time.Minute}
	proxied = &http.Client{Timeout: 5 * time.Minute}

	proxyURL := cfg.Proxy.HTTPS
	if proxyURL == "" {
		proxyURL = cfg.Proxy.HTTP
	}

	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			log.Printf("warning: invalid proxy URL %q: %v", proxyURL, err)
		} else {
			proxied = &http.Client{
				Timeout:   5 * time.Minute,
				Transport: &http.Transport{Proxy: http.ProxyURL(parsed)},
			}
		}
	}

	return
}

// cmdServe starts the MCP HTTP server with graceful shutdown.
func cmdServe(args []string) {
	cfg, _ := loadConfig(args)
	store := openStore(cfg)
	defer store.Close()

	proxied, direct := buildClients(cfg)

	sched := scheduler.New(func(name string) {
		for _, src := range cfg.Sources {
			for _, ref := range src.Refs {
				libName := config.LibraryName(src, ref)
				if libName != name {
					continue
				}
				client := proxied
				if !src.Proxy {
					client = direct
				}
				prov, err := source.NewProvider(src, ref, client)
				if err != nil {
					log.Printf("[scheduler] %s: %v", name, err)
					return
				}
				fetcher := source.NewFetcher(store, cfg.DataDir)
				result, err := fetcher.FetchSource(context.Background(), src, ref, libName, prov)
				if err != nil {
					log.Printf("[scheduler] %s: %v", name, err)
					return
				}
				if result.Updated {
					log.Printf("[scheduler] %s: updated %d chunks", name, result.ChunkCount)
				}
			}
		}
	})

	for _, src := range cfg.Sources {
		if src.Schedule != "" {
			for _, ref := range src.Refs {
				libName := config.LibraryName(src, ref)
				if err := sched.Add(libName, src.Schedule); err != nil {
					log.Printf("warning: %v", err)
				}
			}
		}
	}
	sched.Start()

	srv := mcpsrv.NewServer(store, version)

	httpSrv := &http.Server{
		Addr:    cfg.Listen,
		Handler: srv.Handler(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("docserve %s listening on %s", version, cfg.Listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	sched.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

// cmdFetch fetches and indexes documentation from one or all configured sources.
func cmdFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	sourceName := fs.String("source", "", "Only fetch this source (by name)")
	force := fs.Bool("force", false, "Force re-fetch even if up to date")
	fs.Parse(args) //nolint:errcheck

	resolved, err := config.ResolvePath(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	store := openStore(cfg)
	defer store.Close()

	proxied, direct := buildClients(cfg)

	fetcher := source.NewFetcher(store, cfg.DataDir)
	fetcher.Force = *force

	ctx := context.Background()

	for _, src := range cfg.Sources {
		client := proxied
		if !src.Proxy {
			client = direct
		}

		for _, ref := range src.Refs {
			libName := config.LibraryName(src, ref)

			if *sourceName != "" && libName != *sourceName && src.Name != *sourceName {
				continue
			}

			authClient := source.WrapClientAuth(client, src.Auth)
			prov, err := source.NewProvider(src, ref, authClient)
			if err != nil {
				log.Printf("[%s] error creating provider: %v", libName, err)
				continue
			}

			result, err := fetcher.FetchSource(ctx, src, ref, libName, prov)
			if err != nil {
				log.Printf("[%s] fetch error: %v", libName, err)
				continue
			}

			if result.Updated {
				log.Printf("[%s] updated: sha=%s chunks=%d", result.Source, result.SHA, result.ChunkCount)
			} else {
				log.Printf("[%s] already up to date: sha=%s", result.Source, result.SHA)
			}
		}
	}
}

// cmdList prints a table of all indexed libraries.
func cmdList(args []string) {
	cfg, _ := loadConfig(args)
	store := openStore(cfg)
	defer store.Close()

	libs, err := store.ListLibraries()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing libraries: %v\n", err)
		os.Exit(1)
	}

	if len(libs) == 0 {
		fmt.Println("No libraries indexed.")
		return
	}

	fmt.Printf("%-30s %-40s %-20s %s\n", "NAME", "REPO", "REF", "FETCHED AT")
	fmt.Printf("%-30s %-40s %-20s %s\n", "----", "----", "---", "----------")
	for _, lib := range libs {
		fmt.Printf("%-30s %-40s %-20s %s\n",
			lib.Name,
			lib.Repo,
			lib.Ref,
			lib.FetchedAt.Format(time.RFC3339),
		)
	}
}

// cmdSearch searches a library's indexed documentation.
func cmdSearch(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	maxTokens := fs.Int("max-tokens", 5000, "Maximum token budget for results")
	fs.Parse(args) //nolint:errcheck

	remaining := fs.Args()
	if len(remaining) < 2 {
		fmt.Fprintf(os.Stderr, "usage: docserve search [--config <path>] [--max-tokens <n>] <library> <query...>\n")
		os.Exit(1)
	}

	libraryName := remaining[0]
	query := ""
	for i, w := range remaining[1:] {
		if i > 0 {
			query += " "
		}
		query += w
	}

	resolved, err := config.ResolvePath(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	store := openStore(cfg)
	defer store.Close()

	lib, err := store.GetLibrary(libraryName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	results, err := store.SearchDocs(lib.ID, query, *maxTokens)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error searching: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Printf("No results for %q in library %q.\n", query, libraryName)
		return
	}

	for i, r := range results {
		fmt.Printf("--- Result %d: %s", i+1, r.Path)
		if r.Breadcrumb != "" {
			fmt.Printf(" (%s)", r.Breadcrumb)
		}
		fmt.Println()
		fmt.Println(r.Content)
		fmt.Println()
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "fetch":
		cmdFetch(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	case "search":
		cmdSearch(os.Args[2:])
	case "version":
		fmt.Printf("docserve %s\n", version)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "docserve: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}
