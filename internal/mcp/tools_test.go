package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/codanael/docserve/internal/index"
)

// setupTestStore creates an in-memory store with 2 libraries and some chunks.
func setupTestStore(t *testing.T) *index.Store {
	t.Helper()
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	springID, err := store.UpsertLibrary(index.Library{
		Name:      "spring-boot",
		Repo:      "github.com/spring-projects/spring-boot",
		Ref:       "v3.2.0",
		CommitSHA: "abc123",
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	})
	if err != nil {
		t.Fatalf("UpsertLibrary spring-boot: %v", err)
	}
	err = store.ReplaceChunks(springID, []index.Chunk{
		{Path: "docs/actuator.md", Breadcrumb: "Spring Boot Actuator", Content: "Spring Boot Actuator provides health endpoint for monitoring."},
		{Path: "docs/auto-config.md", Breadcrumb: "Auto Configuration", Content: "Spring Boot auto-configures beans based on the classpath."},
	})
	if err != nil {
		t.Fatalf("ReplaceChunks spring-boot: %v", err)
	}

	angularID, err := store.UpsertLibrary(index.Library{
		Name:      "angular",
		Repo:      "github.com/angular/angular",
		Ref:       "v17.0.0",
		CommitSHA: "def456",
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	})
	if err != nil {
		t.Fatalf("UpsertLibrary angular: %v", err)
	}
	err = store.ReplaceChunks(angularID, []index.Chunk{
		{Path: "docs/components.md", Breadcrumb: "Components", Content: "Angular components are the building blocks of Angular applications."},
	})
	if err != nil {
		t.Fatalf("ReplaceChunks angular: %v", err)
	}

	return store
}

func makeRequest(params map[string]any) mcplib.CallToolRequest {
	req := mcplib.CallToolRequest{}
	req.Params.Arguments = params
	return req
}

func TestListLibraries(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.ListLibraries(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("ListLibraries error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error")
	}

	// Verify structuredContent is set.
	if result.StructuredContent == nil {
		t.Fatal("expected structuredContent to be set")
	}
	list, ok := result.StructuredContent.(libraryList)
	if !ok {
		t.Fatalf("structuredContent type = %T, want libraryList", result.StructuredContent)
	}
	if len(list.Libraries) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(list.Libraries))
	}
	if list.Libraries[0].Name != "angular" || list.Libraries[1].Name != "spring-boot" {
		t.Errorf("unexpected order: %+v", list.Libraries)
	}
	if list.Libraries[0].Repo != "github.com/angular/angular" || list.Libraries[0].CommitSHA != "def456" {
		t.Errorf("repo/commit_sha not populated: %+v", list.Libraries[0])
	}

	// Verify text fallback is valid JSON.
	text := result.Content[0].(mcplib.TextContent).Text
	var fallback map[string]any
	if err := json.Unmarshal([]byte(text), &fallback); err != nil {
		t.Fatalf("text fallback is not valid JSON: %v", err)
	}
	if !strings.Contains(text, "\"libraries\"") {
		t.Errorf("text fallback missing libraries key: %s", text)
	}
}

func TestResolveLibrary(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.ResolveLibrary(context.Background(), makeRequest(map[string]any{"query": "spring"}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error")
	}

	// Verify structuredContent.
	if result.StructuredContent == nil {
		t.Fatal("expected structuredContent to be set")
	}
	match, ok := result.StructuredContent.(libMatch)
	if !ok {
		t.Fatalf("structuredContent type = %T, want libMatch", result.StructuredContent)
	}
	if match.Name != "spring-boot" {
		t.Errorf("name = %q, want spring-boot", match.Name)
	}

	// Verify text fallback.
	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "spring-boot") {
		t.Errorf("text fallback missing spring-boot: %s", text)
	}
}

func TestResolveLibraryNotFound(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.ResolveLibrary(context.Background(), makeRequest(map[string]any{"query": "nonexistent"}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true")
	}
}

func TestGetLibraryDocs(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.GetLibraryDocs(context.Background(), makeRequest(map[string]any{
		"library":    "spring-boot",
		"query":      "health endpoint",
		"max_tokens": 5000,
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content[0].(mcplib.TextContent).Text)
	}

	// get-library-docs returns human-readable text, not JSON.
	text := result.Content[0].(mcplib.TextContent).Text

	// Should have a markdown title.
	if !strings.Contains(text, "# spring-boot") {
		t.Errorf("expected markdown title, got:\n%s", text[:min(200, len(text))])
	}

	// Should contain the doc content.
	if !strings.Contains(strings.ToLower(text), "health") {
		t.Error("expected results to contain 'health'")
	}

	// Should contain breadcrumb as heading.
	if !strings.Contains(text, "## Spring Boot Actuator") {
		t.Errorf("expected breadcrumb heading, got:\n%s", text[:min(300, len(text))])
	}

	// Should contain source path.
	if !strings.Contains(text, "docs/actuator.md") {
		t.Error("expected source path in output")
	}

	// Should NOT be JSON.
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		t.Error("get-library-docs should return text, not JSON")
	}

	// structuredContent should NOT be set for docs.
	if result.StructuredContent != nil {
		t.Error("get-library-docs should not set structuredContent")
	}
}

func TestGetLibraryDocsNoResults(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.GetLibraryDocs(context.Background(), makeRequest(map[string]any{
		"library": "spring-boot",
		"query":   "xyznonexistent",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.IsError {
		t.Fatal("no-results is not an error, just empty")
	}
	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "No results") {
		t.Errorf("expected 'No results' message, got: %s", text)
	}
}

func TestResolveLibraryMissingQuery(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	for name, args := range map[string]map[string]any{
		"absent": {},
		"empty":  {"query": "   "},
		"number": {"query": 42},
	} {
		result, err := h.ResolveLibrary(context.Background(), makeRequest(args))
		if err != nil {
			t.Fatalf("%s: error: %v", name, err)
		}
		if !result.IsError {
			t.Errorf("%s: expected IsError=true", name)
		}
		if text := result.Content[0].(mcplib.TextContent).Text; !strings.Contains(text, "query") {
			t.Errorf("%s: error text should mention the argument, got %q", name, text)
		}
	}
}

func TestGetLibraryDocsMissingArgs(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	cases := map[string]map[string]any{
		"no library":      {"query": "actuator"},
		"no query":        {"library": "spring-boot"},
		"empty query":     {"library": "spring-boot", "query": ""},
		"zero tokens":     {"library": "spring-boot", "query": "actuator", "max_tokens": 0},
		"negative tokens": {"library": "spring-boot", "query": "actuator", "max_tokens": -5},
	}
	for name, args := range cases {
		result, err := h.GetLibraryDocs(context.Background(), makeRequest(args))
		if err != nil {
			t.Fatalf("%s: error: %v", name, err)
		}
		if !result.IsError {
			t.Errorf("%s: expected IsError=true, got %q", name, result.Content[0].(mcplib.TextContent).Text)
		}
	}
}

func TestGetLibraryDocsTruncationNotice(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	lib, err := store.GetLibrary(context.Background(), "spring-boot")
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	chunks := make([]index.Chunk, 60)
	for i := range chunks {
		chunks[i] = index.Chunk{Path: "docs/p.md", Breadcrumb: "P", Content: "trunc keyword " + strings.Repeat("x", 200)}
	}
	if err := store.ReplaceChunks(lib.ID, chunks); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	result, err := h.GetLibraryDocs(context.Background(), makeRequest(map[string]any{"library": "spring-boot", "query": "trunc", "max_tokens": 120}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := result.Content[0].(mcplib.TextContent).Text
	if !strings.Contains(text, "max_tokens") {
		t.Errorf("expected truncation notice mentioning max_tokens, got:\n%s", text)
	}
}

func TestGetLibraryDocsNotFound(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.GetLibraryDocs(context.Background(), makeRequest(map[string]any{
		"library": "unknown-library",
		"query":   "something",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true")
	}
}
