package index

import (
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

	results, err := s.SearchDocs(libID, "database configuration", 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	// The chunk with "Database Configuration" in the breadcrumb should rank first
	// because breadcrumb has weight 2.0 vs content weight 1.0.
	if results[0].Path != "docs/db.md" {
		t.Errorf("expected docs/db.md to rank first (breadcrumb boost), got %q", results[0].Path)
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

	results, err := s.SearchDocs(libID, "budget test keyword", 100)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if len(results) > 5 {
		t.Errorf("expected <=5 results with maxTokens=100, got %d", len(results))
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

	results, err := s.SearchDocs(libID, "xyznonexistentterm", 10000)
	if err != nil {
		t.Fatalf("SearchDocs error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"database configuration", "database OR configuration"},
		{"single", "single"},
		{"  spaced  words  ", "spaced OR words"},
		{"", ""},
		{"e.g. configuration", "eg OR configuration"},
		{"v3.2.0", "v320"},
		{"query with (parens)", "query OR with OR parens"},
		{"dots.in.words special*chars", "dotsinwords OR specialchars"},
		{`"quoted" terms`, "quoted OR terms"},
		{"...", ""},
		{"hello --- world", "hello OR world"},
		{"colon:separated", "colonseparated"},
	}

	for _, tc := range tests {
		got := buildFTSQuery(tc.input)
		if got != tc.want {
			t.Errorf("buildFTSQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSearchSpecialCharacters(t *testing.T) {
	s, libID := testLibrary(t)
	chunks := []Chunk{
		{Path: "docs/intro.md", Breadcrumb: "Introduction", Content: "Welcome to version 3.2 of the library."},
	}
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}
	// These queries previously caused FTS5 parse errors.
	for _, q := range []string{"v3.2", "e.g.", "(test)", `"quoted"`, "...", "hello-world"} {
		_, err := s.SearchDocs(libID, q, 10000)
		if err != nil {
			t.Errorf("SearchDocs(%q) unexpected error: %v", q, err)
		}
	}
}
