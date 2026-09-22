package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/codanael/docserve/internal/index"
)

// defaultMaxTokens is the token budget used by get-library-docs when the
// caller does not provide max_tokens.
const defaultMaxTokens = 5000

// ToolHandlers holds the store and provides MCP tool handler methods.
type ToolHandlers struct {
	Store *index.Store
}

// structuredResult builds a CallToolResult carrying v as structuredContent
// plus the same JSON as a text fallback for clients without structured
// output support.
func structuredResult(v any) *mcplib.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return mcplib.NewToolResultError("marshal result: " + err.Error())
	}
	return mcplib.NewToolResultStructured(v, string(data))
}

// requireText returns the named string argument, or an error result when it
// is missing, not a string, or blank.
func requireText(req mcplib.CallToolRequest, key string) (string, *mcplib.CallToolResult) {
	v, err := req.RequireString(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return "", mcplib.NewToolResultError(fmt.Sprintf("argument %q is required and must be a non-empty string", key))
	}
	return v, nil
}

// libEntry is one row of the list-libraries output.
type libEntry struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Ref       string `json:"ref"`
	CommitSHA string `json:"commit_sha"`
	FetchedAt string `json:"fetched_at"`
}

// libraryList is the structured output of list-libraries.
type libraryList struct {
	Libraries []libEntry `json:"libraries"`
}

// ListLibraries returns all indexed libraries.
func (h *ToolHandlers) ListLibraries(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	libs, err := h.Store.ListLibraries(ctx)
	if err != nil {
		return mcplib.NewToolResultError("failed to list libraries: " + err.Error()), nil
	}

	out := libraryList{Libraries: make([]libEntry, 0, len(libs))}
	for _, lib := range libs {
		out.Libraries = append(out.Libraries, libEntry{
			Name:      lib.Name,
			Repo:      lib.Repo,
			Ref:       lib.Ref,
			CommitSHA: lib.CommitSHA,
			FetchedAt: lib.FetchedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return structuredResult(out), nil
}

// libMatch is the structured output of resolve-library.
type libMatch struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

// ResolveLibrary finds a library by name query and returns the first match.
func (h *ToolHandlers) ResolveLibrary(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	query, errResult := requireText(req, "query")
	if errResult != nil {
		return errResult, nil
	}

	libs, err := h.Store.FindLibraries(ctx, query)
	if err != nil {
		return mcplib.NewToolResultError("failed to find libraries: " + err.Error()), nil
	}
	if len(libs) == 0 {
		return mcplib.NewToolResultError(fmt.Sprintf("no library found matching %q; call list-libraries to see what is indexed", query)), nil
	}

	return structuredResult(libMatch{Name: libs[0].Name, Ref: libs[0].Ref}), nil
}

// GetLibraryDocs searches a library's documentation and returns human-readable markdown.
func (h *ToolHandlers) GetLibraryDocs(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	library, errResult := requireText(req, "library")
	if errResult != nil {
		return errResult, nil
	}
	query, errResult := requireText(req, "query")
	if errResult != nil {
		return errResult, nil
	}
	maxTokens := req.GetInt("max_tokens", defaultMaxTokens)
	if maxTokens < 1 {
		return mcplib.NewToolResultError("argument \"max_tokens\" must be a positive integer"), nil
	}

	lib, err := h.Store.GetLibrary(ctx, library)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("library %q not found; use resolve-library or list-libraries to get the exact name", library)), nil
	}

	out, err := h.Store.SearchDocs(ctx, lib.ID, query, maxTokens)
	if err != nil {
		return mcplib.NewToolResultError("search failed: " + err.Error()), nil
	}

	if len(out.Results) == 0 {
		return mcplib.NewToolResultText(fmt.Sprintf("No results found for %q in %s (%s).", query, lib.Name, lib.Ref)), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s (%s) — %d results\n\n", lib.Name, lib.Ref, len(out.Results))
	for i, r := range out.Results {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "## %s\n", r.Breadcrumb)
		fmt.Fprintf(&b, "_Source: %s_\n\n", r.Path)
		b.WriteString(r.Content)
		b.WriteByte('\n')
	}
	if out.Truncated {
		b.WriteString("\n---\n\n_More matching chunks were omitted by the token budget or the result limit. Refine the query or raise max_tokens to see them._\n")
	}

	return mcplib.NewToolResultText(b.String()), nil
}
