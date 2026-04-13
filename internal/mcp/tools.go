package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/codanael/docserve/internal/index"
)

// ToolHandlers holds the store and provides MCP tool handler methods.
type ToolHandlers struct {
	Store *index.Store
}

// structuredResult builds a CallToolResult with both structuredContent (for
// clients that support it) and a JSON text fallback (for those that don't).
func structuredResult(v any) (*mcplib.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return &mcplib.CallToolResult{
		Content:           []mcplib.Content{mcplib.TextContent{Type: "text", Text: string(data)}},
		StructuredContent: v,
	}, nil
}

type libEntry struct {
	Name      string `json:"name"`
	Ref       string `json:"ref"`
	FetchedAt string `json:"fetched_at"`
}

type listLibrariesResult struct {
	Libraries []libEntry `json:"libraries"`
}

// ListLibraries returns all indexed libraries.
func (h *ToolHandlers) ListLibraries(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	libs, err := h.Store.ListLibraries()
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("failed to list libraries: %v", err)), nil
	}

	entries := make([]libEntry, 0, len(libs))
	for _, lib := range libs {
		entries = append(entries, libEntry{
			Name:      lib.Name,
			Ref:       lib.Ref,
			FetchedAt: lib.FetchedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	result, err := structuredResult(listLibrariesResult{Libraries: entries})
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

type libMatch struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

// ResolveLibrary finds a library by name query and returns the first match.
func (h *ToolHandlers) ResolveLibrary(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	query := req.GetString("query", "")

	libs, err := h.Store.FindLibraries(query)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("failed to find libraries: %v", err)), nil
	}

	if len(libs) == 0 {
		return mcplib.NewToolResultError(fmt.Sprintf("no library found matching %q", query)), nil
	}

	match := libMatch{Name: libs[0].Name, Ref: libs[0].Ref}

	result, err := structuredResult(match)
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	return result, nil
}

// GetLibraryDocs searches a library's documentation and returns human-readable text.
func (h *ToolHandlers) GetLibraryDocs(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	library := req.GetString("library", "")
	query := req.GetString("query", "")
	maxTokens := req.GetInt("max_tokens", 5000)

	lib, err := h.Store.GetLibrary(library)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("library %q not found", library)), nil
	}

	results, err := h.Store.SearchDocs(lib.ID, query, maxTokens)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	if len(results) == 0 {
		return mcplib.NewToolResultText(fmt.Sprintf("No results found for %q in %s (%s).", query, lib.Name, lib.Ref)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s) — %d results\n\n", lib.Name, lib.Ref, len(results))

	for i, r := range results {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "## %s\n", r.Breadcrumb)
		fmt.Fprintf(&b, "_Source: %s_\n\n", r.Path)
		b.WriteString(r.Content)
		b.WriteByte('\n')
	}

	return mcplib.NewToolResultText(b.String()), nil
}
