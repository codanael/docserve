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
	t.Cleanup(func() { store.Close() })

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
	entries, ok := result.StructuredContent.([]libEntry)
	if !ok {
		t.Fatalf("structuredContent type = %T, want []libEntry", result.StructuredContent)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "angular" {
		t.Errorf("first entry name = %q, want angular", entries[0].Name)
	}

	// Verify text fallback is valid JSON.
	text := result.Content[0].(mcplib.TextContent).Text
	var fallback []map[string]any
	if err := json.Unmarshal([]byte(text), &fallback); err != nil {
		t.Fatalf("text fallback is not valid JSON: %v", err)
	}
	if len(fallback) != 2 {
		t.Errorf("text fallback has %d entries, want 2", len(fallback))
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
