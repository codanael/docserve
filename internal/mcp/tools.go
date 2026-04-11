package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/codanael/docserve/internal/index"
)

// ToolHandlers holds the store and provides MCP tool handler methods.
type ToolHandlers struct {
	Store *index.Store
}

// ListLibraries returns all indexed libraries as a JSON array.
func (h *ToolHandlers) ListLibraries(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	libs, err := h.Store.ListLibraries()
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("failed to list libraries: %v", err)), nil
	}

	type libEntry struct {
		Name      string `json:"name"`
		Ref       string `json:"ref"`
		FetchedAt string `json:"fetched_at"`
	}

	entries := make([]libEntry, 0, len(libs))
	for _, lib := range libs {
		entries = append(entries, libEntry{
			Name:      lib.Name,
			Ref:       lib.Ref,
			FetchedAt: lib.FetchedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("failed to encode libraries: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(data)), nil
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

	type libMatch struct {
		Name string `json:"name"`
		Ref  string `json:"ref"`
	}

	match := libMatch{
		Name: libs[0].Name,
		Ref:  libs[0].Ref,
	}

	data, err := json.Marshal(match)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("failed to encode result: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(data)), nil
}

// GetLibraryDocs searches a library's documentation and returns matching chunks.
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

	type resultEntry struct {
		Path       string  `json:"path"`
		Breadcrumb string  `json:"breadcrumb"`
		Content    string  `json:"content"`
		Score      float64 `json:"score"`
	}

	type searchResponse struct {
		Library string        `json:"library"`
		Version string        `json:"version"`
		Results []resultEntry `json:"results"`
	}

	entries := make([]resultEntry, 0, len(results))
	for _, r := range results {
		entries = append(entries, resultEntry{
			Path:       r.Path,
			Breadcrumb: r.Breadcrumb,
			Content:    r.Content,
			Score:      r.Score,
		})
	}

	resp := searchResponse{
		Library: lib.Name,
		Version: lib.Ref,
		Results: entries,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("failed to encode results: %v", err)), nil
	}

	return mcplib.NewToolResultText(string(data)), nil
}
