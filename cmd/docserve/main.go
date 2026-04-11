package main

import (
	"fmt"
	"os"
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

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		fmt.Fprintln(os.Stderr, "serve: not yet implemented")
		os.Exit(1)
	case "fetch":
		fmt.Fprintln(os.Stderr, "fetch: not yet implemented")
		os.Exit(1)
	case "list":
		fmt.Fprintln(os.Stderr, "list: not yet implemented")
		os.Exit(1)
	case "search":
		fmt.Fprintln(os.Stderr, "search: not yet implemented")
		os.Exit(1)
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
