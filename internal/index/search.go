package index

import (
	"context"
	"fmt"
	"strings"
)

// buildFTSQuery transforms a plain text query into an FTS5 OR query in which
// every whitespace-separated term is quoted as an FTS5 string, so operators,
// parentheses and column filters typed by the caller are matched literally.
// "database configuration" → `"database" OR "configuration"`
// Empty input returns "".
func buildFTSQuery(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 {
		return ""
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " OR ")
}

// maxSearchRows caps the number of chunks considered for a single search.
const maxSearchRows = 50

// SearchDocs performs an FTS5 search using BM25 ranking and returns results
// within the token budget (approximated as len(content)/4 tokens). The first
// result is always returned even if it exceeds the budget. Truncated is set
// when matching chunks were left out.
func (s *Store) SearchDocs(ctx context.Context, libraryID int64, query string, maxTokens int) (SearchOutput, error) {
	var out SearchOutput

	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return out, nil
	}

	const q = `
		SELECT path, breadcrumb, content, bm25(chunks, 0.0, 1.5, 2.0, 1.0) AS score
		FROM chunks
		WHERE library_id = ? AND chunks MATCH ?
		ORDER BY score ASC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, libraryID, ftsQuery, maxSearchRows+1)
	if err != nil {
		return out, fmt.Errorf("fts search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tokensUsed := 0
	for rows.Next() {
		if len(out.Results) == maxSearchRows {
			out.Truncated = true
			break
		}
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Breadcrumb, &r.Content, &r.Score); err != nil {
			return SearchOutput{}, fmt.Errorf("scan search result: %w", err)
		}
		tokens := len(r.Content) / 4
		if tokensUsed+tokens > maxTokens && len(out.Results) > 0 {
			out.Truncated = true
			break
		}
		tokensUsed += tokens
		out.Results = append(out.Results, r)
	}
	if err := rows.Err(); err != nil {
		return SearchOutput{}, fmt.Errorf("search rows: %w", err)
	}
	return out, nil
}
