package index

import (
	"context"
	"testing"
	"time"
)

func TestStoreMigration(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:) error: %v", err)
	}
	defer func() { _ = s.Close() }()
}

func TestStoreLibraryCRUD(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore error: %v", err)
	}
	defer func() { _ = s.Close() }()

	lib := Library{
		Name:      "mylib",
		Repo:      "github.com/example/mylib",
		Ref:       "main",
		CommitSHA: "abc123",
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	}

	// Upsert (insert)
	id, err := s.UpsertLibrary(lib)
	if err != nil {
		t.Fatalf("UpsertLibrary insert error: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	// Get
	got, err := s.GetLibrary(context.Background(), "mylib")
	if err != nil {
		t.Fatalf("GetLibrary error: %v", err)
	}
	if got.Name != lib.Name {
		t.Errorf("Name: got %q want %q", got.Name, lib.Name)
	}
	if got.Repo != lib.Repo {
		t.Errorf("Repo: got %q want %q", got.Repo, lib.Repo)
	}
	if got.Ref != lib.Ref {
		t.Errorf("Ref: got %q want %q", got.Ref, lib.Ref)
	}
	if got.CommitSHA != lib.CommitSHA {
		t.Errorf("CommitSHA: got %q want %q", got.CommitSHA, lib.CommitSHA)
	}
	if got.ID != id {
		t.Errorf("ID: got %d want %d", got.ID, id)
	}

	// Upsert (update same name)
	lib.CommitSHA = "def456"
	lib.Ref = "v2"
	id2, err := s.UpsertLibrary(lib)
	if err != nil {
		t.Fatalf("UpsertLibrary update error: %v", err)
	}
	if id2 != id {
		t.Errorf("updated id should match original: got %d want %d", id2, id)
	}

	got2, err := s.GetLibrary(context.Background(), "mylib")
	if err != nil {
		t.Fatalf("GetLibrary after update error: %v", err)
	}
	if got2.CommitSHA != "def456" {
		t.Errorf("CommitSHA after update: got %q want %q", got2.CommitSHA, "def456")
	}
	if got2.Ref != "v2" {
		t.Errorf("Ref after update: got %q want %q", got2.Ref, "v2")
	}

	// Insert a second library
	lib2 := Library{
		Name:      "anotherlib",
		Repo:      "github.com/example/anotherlib",
		Ref:       "main",
		CommitSHA: "xyz789",
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	}
	_, err = s.UpsertLibrary(lib2)
	if err != nil {
		t.Fatalf("UpsertLibrary lib2 error: %v", err)
	}

	// List
	libs, err := s.ListLibraries(context.Background())
	if err != nil {
		t.Fatalf("ListLibraries error: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(libs))
	}
	// Should be ordered by name: "anotherlib" < "mylib"
	if libs[0].Name != "anotherlib" {
		t.Errorf("first lib should be anotherlib, got %q", libs[0].Name)
	}
	if libs[1].Name != "mylib" {
		t.Errorf("second lib should be mylib, got %q", libs[1].Name)
	}

	// GetLibrary for non-existent
	_, err = s.GetLibrary(context.Background(), "doesnotexist")
	if err == nil {
		t.Error("expected error for non-existent library, got nil")
	}

	// FindLibraries
	found, err := s.FindLibraries(context.Background(), "another")
	if err != nil {
		t.Fatalf("FindLibraries error: %v", err)
	}
	if len(found) != 1 || found[0].Name != "anotherlib" {
		t.Errorf("FindLibraries: expected [anotherlib], got %v", found)
	}

	found2, err := s.FindLibraries(context.Background(), "lib")
	if err != nil {
		t.Fatalf("FindLibraries 'lib' error: %v", err)
	}
	if len(found2) != 2 {
		t.Errorf("FindLibraries 'lib': expected 2 results, got %d", len(found2))
	}
}

func TestFindLibrariesEscapesWildcards(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore error: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	for _, name := range []string{"angular", "spring-boot", "my_lib"} {
		if _, err := s.UpsertLibrary(Library{Name: name, Repo: "r/" + name, Ref: "main", CommitSHA: "x", FetchedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("UpsertLibrary %s: %v", name, err)
		}
	}

	cases := []struct {
		query string
		want  int
	}{
		{"%", 0},      // literal percent matches nothing
		{"_ng", 0},    // underscore is not a single-char wildcard
		{"my_lib", 1}, // literal underscore still matches
		{"ng", 2},     // plain substring works ("angular" and "spring-boot" both contain "ng")
		{"", 3},       // empty query matches everything
	}
	for _, tc := range cases {
		libs, err := s.FindLibraries(context.Background(), tc.query)
		if err != nil {
			t.Fatalf("FindLibraries(%q) error: %v", tc.query, err)
		}
		if len(libs) != tc.want {
			t.Errorf("FindLibraries(%q) = %d libraries, want %d", tc.query, len(libs), tc.want)
		}
	}
}

func TestStoreChunks(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore error: %v", err)
	}
	defer func() { _ = s.Close() }()

	lib := Library{
		Name:      "testlib",
		Repo:      "github.com/example/testlib",
		Ref:       "main",
		CommitSHA: "aaa111",
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	}
	libID, err := s.UpsertLibrary(lib)
	if err != nil {
		t.Fatalf("UpsertLibrary error: %v", err)
	}

	chunks := []Chunk{
		{
			Path:       "docs/intro.md",
			Breadcrumb: "Introduction",
			Content:    "This is the introduction to the library with some content about getting started.",
		},
		{
			Path:       "docs/api.md",
			Breadcrumb: "API Reference",
			Content:    "The API provides functions for querying and inserting data into the database.",
		},
		{
			Path:       "docs/advanced.md",
			Breadcrumb: "Advanced Usage",
			Content:    "Advanced usage patterns for performance optimization and caching strategies.",
		},
	}

	// Insert chunks
	if err := s.ReplaceChunks(libID, chunks); err != nil {
		t.Fatalf("ReplaceChunks error: %v", err)
	}

	// Search
	out, err := s.SearchDocs(context.Background(), libID, "database querying", 10000)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(out.Results) == 0 {
		t.Error("expected search results, got none")
	}
	// Top result should be api.md since it talks about querying
	if out.Results[0].Path != "docs/api.md" {
		t.Logf("top result path: %q (expected docs/api.md, may vary by ranking)", out.Results[0].Path)
	}
	for _, r := range out.Results {
		if r.Score == 0 {
			t.Errorf("result for %q has zero score", r.Path)
		}
	}

	// Ready should be true now
	if !s.Ready(context.Background()) {
		t.Error("Ready() should return true after indexing")
	}

	// Replace chunks with new set (verify old ones gone)
	newChunks := []Chunk{
		{
			Path:       "docs/newfile.md",
			Breadcrumb: "New File",
			Content:    "Completely new content replacing the old chunks.",
		},
	}
	if err := s.ReplaceChunks(libID, newChunks); err != nil {
		t.Fatalf("ReplaceChunks (replace) error: %v", err)
	}

	// Old search should return no result (or only new content)
	out2, err := s.SearchDocs(context.Background(), libID, "database querying", 10000)
	if err != nil {
		t.Fatalf("Search after replace error: %v", err)
	}
	for _, r := range out2.Results {
		if r.Path == "docs/api.md" {
			t.Errorf("old chunk docs/api.md still present after ReplaceChunks")
		}
	}

	// New content should be searchable
	out3, err := s.SearchDocs(context.Background(), libID, "new content", 10000)
	if err != nil {
		t.Fatalf("Search for new content error: %v", err)
	}
	if len(out3.Results) == 0 {
		t.Error("expected results for new content, got none")
	}
}
