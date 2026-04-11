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

// setupTestStore creates an in-memory store with 2 libraries (spring-boot + angular) with some chunks.
func setupTestStore(t *testing.T) *index.Store {
	t.Helper()
	store, err := index.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Insert spring-boot library
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

	// Insert spring-boot chunks
	err = store.ReplaceChunks(springID, []index.Chunk{
		{
			Path:       "docs/actuator.md",
			Breadcrumb: "Spring Boot Actuator",
			Content:    "Spring Boot Actuator provides health endpoint for monitoring. The health endpoint returns application health status.",
		},
		{
			Path:       "docs/auto-config.md",
			Breadcrumb: "Auto Configuration",
			Content:    "Spring Boot auto-configures beans based on the classpath. DataSource auto-configuration sets up database connections.",
		},
	})
	if err != nil {
		t.Fatalf("ReplaceChunks spring-boot: %v", err)
	}

	// Insert angular library
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

	// Insert angular chunks
	err = store.ReplaceChunks(angularID, []index.Chunk{
		{
			Path:       "docs/components.md",
			Breadcrumb: "Components",
			Content:    "Angular components are the building blocks of Angular applications. Each component has a template, styles, and logic.",
		},
		{
			Path:       "docs/services.md",
			Breadcrumb: "Services",
			Content:    "Angular services provide shared functionality across components. Dependency injection makes services available everywhere.",
		},
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
		t.Fatalf("expected success, got error: %v", result.Content[0].(mcplib.TextContent).Text)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	var libs []map[string]any
	if err := json.Unmarshal([]byte(text), &libs); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(libs) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(libs))
	}

	// Verify both libraries are present (ordered by name: angular, spring-boot)
	if libs[0]["name"] != "angular" {
		t.Errorf("expected first library to be 'angular', got %q", libs[0]["name"])
	}
	if libs[1]["name"] != "spring-boot" {
		t.Errorf("expected second library to be 'spring-boot', got %q", libs[1]["name"])
	}
}

func TestResolveLibrary(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.ResolveLibrary(context.Background(), makeRequest(map[string]any{"query": "spring"}))
	if err != nil {
		t.Fatalf("ResolveLibrary error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content[0].(mcplib.TextContent).Text)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	var match map[string]any
	if err := json.Unmarshal([]byte(text), &match); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if match["name"] != "spring-boot" {
		t.Errorf("expected name 'spring-boot', got %q", match["name"])
	}
}

func TestResolveLibraryNotFound(t *testing.T) {
	store := setupTestStore(t)
	h := &ToolHandlers{Store: store}

	result, err := h.ResolveLibrary(context.Background(), makeRequest(map[string]any{"query": "nonexistent"}))
	if err != nil {
		t.Fatalf("ResolveLibrary error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected IsError=true for not found library")
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
		t.Fatalf("GetLibraryDocs error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content[0].(mcplib.TextContent).Text)
	}

	text := result.Content[0].(mcplib.TextContent).Text
	var resp map[string]any
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if resp["library"] != "spring-boot" {
		t.Errorf("expected library 'spring-boot', got %q", resp["library"])
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatal("expected non-empty results")
	}

	// Verify results contain "health"
	found := false
	for _, r := range results {
		entry, ok := r.(map[string]any)
		if !ok {
			continue
		}
		content, _ := entry["content"].(string)
		if strings.Contains(strings.ToLower(content), "health") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected search results to contain 'health'")
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
		t.Fatalf("GetLibraryDocs error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected IsError=true for not found library")
	}
}
