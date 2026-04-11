package index

import (
	"fmt"
	"strings"
)

// buildFTSQuery transforms a plain text query into an FTS5 OR query.
// "database configuration" → "database OR configuration"
// Empty input returns "".
func buildFTSQuery(input string) string {
	words := strings.Fields(input)
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " OR ")
}

// SearchDocs performs an FTS5 search using BM25 ranking and returns results
// within the token budget (approximated as len(content)/4 tokens).
func (s *Store) SearchDocs(libraryID int64, query string, maxTokens int) ([]SearchResult, error) {
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	const q = `
		SELECT path, breadcrumb, content, bm25(chunks, 0.0, 1.5, 2.0, 1.0) AS score
		FROM chunks
		WHERE library_id = ? AND chunks MATCH ?
		ORDER BY score ASC
		LIMIT 50`

	rows, err := s.db.Query(q, libraryID, ftsQuery)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var results []SearchResult
	tokensUsed := 0
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Path, &r.Breadcrumb, &r.Content, &r.Score); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		tokens := len(r.Content) / 4
		if tokensUsed+tokens > maxTokens && len(results) > 0 {
			break
		}
		tokensUsed += tokens
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search rows: %w", err)
	}
	return results, nil
}
