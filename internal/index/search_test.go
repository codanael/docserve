package index

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func testLibrary(t *testing.T) (*Store, int64) {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	libID, err := s.UpsertLibrary(Library{
		Name:      "testlib",
		Repo:      "github.com/example/testlib",
		Ref:       "main",
		CommitSHA: "abc123",
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	})
	if err != nil {
		t.Fatalf("UpsertLibrary error: %v", err)
	}
	return s, libID
}

func TestSearchRanking(t *testing.T) {
	s, libID := testLibrary(t)

	chunks := []Chunk{
		{
			Path:       "docs/general.md",
			Breadcrumb: "General Docs",
			Content:    "This document discusses database configuration in detail with extensive content about configuration management and database tuning parameters.",
		},
		{
			Path:       "docs/db.md",
			Breadcrumb: "Database Configuration",
			Content:    "Some content about getting started.",
		},
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "database configuration", 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected results, got none")
	}
	// The chunk with "Database Configuration" in the breadcrumb should rank first
	// because breadcrumb has weight 2.0 vs content weight 1.0.
	if out.Results[0].Path != "docs/db.md" {
		t.Errorf("expected docs/db.md to rank first (breadcrumb boost), got %q", out.Results[0].Path)
	}
}

func TestSearchTokenBudget(t *testing.T) {
	s, libID := testLibrary(t)

	// Create 20 chunks each with ~80 characters of content (~20 tokens each).
	chunks := make([]Chunk, 20)
	for i := range chunks {
		chunks[i] = Chunk{
			Path:       fmt.Sprintf("docs/page%02d.md", i),
			Breadcrumb: fmt.Sprintf("Page %d", i),
			// exactly 80 chars = 20 tokens; 5 results * 20 = 100 tokens, 6th exceeds budget
			Content: fmt.Sprintf("budget test keyword content page number %02d fill text here padding end ok yeah123", i),
		}
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "budget test keyword", 100)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if len(out.Results) > 5 {
		t.Errorf("expected <=5 results with maxTokens=100, got %d", len(out.Results))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true when the budget cuts results")
	}
}

func TestSearchRowLimit(t *testing.T) {
	s, libID := testLibrary(t)

	chunks := make([]Chunk, maxSearchRows+10)
	for i := range chunks {
		chunks[i] = Chunk{
			Path:       fmt.Sprintf("docs/page%03d.md", i),
			Breadcrumb: fmt.Sprintf("Page %d", i),
			Content:    fmt.Sprintf("rowlimit keyword number %03d", i),
		}
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "rowlimit", 1_000_000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) != maxSearchRows {
		t.Errorf("expected %d results, got %d", maxSearchRows, len(out.Results))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true when more rows exist than the row limit")
	}
}

func TestSearchNotTruncated(t *testing.T) {
	s, libID := testLibrary(t)
	if err := s.ReplaceChunks(libID, []Chunk{
		{Path: "a.md", Breadcrumb: "A", Content: "small corpus alpha"},
		{Path: "b.md", Breadcrumb: "B", Content: "small corpus beta"},
	}); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "corpus", 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) != 2 || out.Truncated {
		t.Errorf("expected 2 results and Truncated=false, got %d results, Truncated=%v", len(out.Results), out.Truncated)
	}
}

func TestSearchNoResults(t *testing.T) {
	s, libID := testLibrary(t)

	chunks := []Chunk{
		{
			Path:       "docs/intro.md",
			Breadcrumb: "Introduction",
			Content:    "Welcome to the library documentation.",
		},
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	out, err := s.SearchDocs(context.Background(), libID, "xyznonexistentterm", 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(out.Results))
	}
}

func TestSearchSpecialCharacters(t *testing.T) {
	s, libID := testLibrary(t)

	chunks := []Chunk{
		{Path: "docs/health.md", Breadcrumb: "Health", Content: "The health endpoint reports status."},
		{Path: "docs/intro.md", Breadcrumb: "Introduction", Content: "Welcome to version v3.2 of the library, e.g. the docs."},
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	// None of these may reach FTS5 as operators; they must be matched literally.
	for _, q := range []string{
		`health AND`, `NEAR(`, `"health`, `path:health`, `health OR NOT`, `(health`,
		"v3.2", "e.g.", "(test)", `"quoted"`, "...", "hello-world",
	} {
		if _, err := s.SearchDocs(context.Background(), libID, q, 10000); err != nil {
			t.Errorf("SearchDocs(%q) returned error: %v", q, err)
		}
	}

	out, err := s.SearchDocs(context.Background(), libID, `health AND`, 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(out.Results) != 1 {
		t.Errorf("expected 1 result for literal search, got %d", len(out.Results))
	}

	// Quoting keeps the tokenizer's own behaviour: "v3.2" still tokenizes to
	// v3/2 and "e.g." to e/g, so both still match the indexed text.
	for _, q := range []string{"v3.2", "e.g."} {
		out, err = s.SearchDocs(context.Background(), libID, q, 10000)
		if err != nil {
			t.Fatalf("SearchDocs(%q) error: %v", q, err)
		}
		if len(out.Results) != 1 {
			t.Errorf("expected 1 result for %q, got %d", q, len(out.Results))
		}
	}
}

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"database configuration", `"database" OR "configuration"`},
		{"single", `"single"`},
		{"  spaced  words  ", `"spaced" OR "words"`},
		{"", ""},
		{`say "hi"`, `"say" OR """hi"""`},
		{"health AND", `"health" OR "AND"`},
		{"path:foo NEAR(", `"path:foo" OR "NEAR("`},
		{"e.g. configuration", `"e.g." OR "configuration"`},
		{"v3.2.0", `"v3.2.0"`},
		{"query with (parens)", `"query" OR "with" OR "(parens)"`},
		{"dots.in.words special*chars", `"dots.in.words" OR "special*chars"`},
		{"...", `"..."`},
		{"hello --- world", `"hello" OR "---" OR "world"`},
	}

	for _, tc := range tests {
		got := buildFTSQuery(tc.input)
		if got != tc.want {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSearchDocsCancelledContext(t *testing.T) {
	s, libID := testLibrary(t)
	if err := s.ReplaceChunks(libID, []Chunk{{Path: "a.md", Breadcrumb: "A", Content: "alpha beta"}}); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.SearchDocs(ctx, libID, "alpha", 1000); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if _, err := s.ListLibraries(ctx); err == nil {
		t.Fatal("expected error from cancelled context on ListLibraries, got nil")
	}
}
