package index

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Library represents an indexed documentation library.
type Library struct {
	ID        int64
	Name      string
	Repo      string
	Ref       string
	CommitSHA string
	FetchedAt time.Time
}

// Chunk represents a piece of documentation content.
type Chunk struct {
	Path       string
	Breadcrumb string
	Content    string
}

// SearchResult represents a single result from an FTS search.
type SearchResult struct {
	Path       string
	Breadcrumb string
	Content    string
	Score      float64
}

// Store holds the SQLite database connection.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the SQLite database at dsn, enables WAL mode,
// and runs schema migrations.
func OpenStore(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dsn, err)
	}

	// Enable WAL mode for better concurrency.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the required tables and FTS5 virtual tables if they don't exist.
func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS libraries (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT    NOT NULL UNIQUE,
			repo       TEXT    NOT NULL,
			ref        TEXT    NOT NULL,
			commit_sha TEXT    NOT NULL,
			fetched_at INTEGER NOT NULL
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(
			library_id UNINDEXED,
			path,
			breadcrumb,
			content,
			tokenize='porter unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS chunk_meta (
			rowid      INTEGER PRIMARY KEY,
			library_id INTEGER NOT NULL REFERENCES libraries(id),
			path       TEXT    NOT NULL,
			breadcrumb TEXT    NOT NULL,
			byte_size  INTEGER NOT NULL
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec migration: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// UpsertLibrary inserts or updates a library record and returns its id.
func (s *Store) UpsertLibrary(lib Library) (int64, error) {
	const q = `
		INSERT INTO libraries (name, repo, ref, commit_sha, fetched_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			repo       = excluded.repo,
			ref        = excluded.ref,
			commit_sha = excluded.commit_sha,
			fetched_at = excluded.fetched_at
		RETURNING id`

	var id int64
	err := s.db.QueryRow(q,
		lib.Name,
		lib.Repo,
		lib.Ref,
		lib.CommitSHA,
		lib.FetchedAt.Unix(),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert library %q: %w", lib.Name, err)
	}
	return id, nil
}

// GetLibrary returns the library with the given name, or an error if not found.
func (s *Store) GetLibrary(name string) (*Library, error) {
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name = ?`
	row := s.db.QueryRow(q, name)

	var lib Library
	var fetchedAt int64
	if err := row.Scan(&lib.ID, &lib.Name, &lib.Repo, &lib.Ref, &lib.CommitSHA, &fetchedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("library %q not found", name)
		}
		return nil, fmt.Errorf("get library %q: %w", name, err)
	}
	lib.FetchedAt = time.Unix(fetchedAt, 0).UTC()
	return &lib, nil
}

// ListLibraries returns all libraries ordered by name.
func (s *Store) ListLibraries() ([]Library, error) {
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries ORDER BY name`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var lib Library
		var fetchedAt int64
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Repo, &lib.Ref, &lib.CommitSHA, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan library row: %w", err)
		}
		lib.FetchedAt = time.Unix(fetchedAt, 0).UTC()
		libs = append(libs, lib)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list libraries rows: %w", err)
	}
	return libs, nil
}

// ReplaceChunks transactionally deletes all existing chunks for a library and
// inserts the new ones into both the FTS5 chunks table and chunk_meta.
func (s *Store) ReplaceChunks(libraryID int64, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete existing chunks for this library from FTS table.
	if _, err := tx.Exec(`DELETE FROM chunks WHERE library_id = ?`, libraryID); err != nil {
		return fmt.Errorf("delete old chunks: %w", err)
	}

	// Delete existing chunk_meta for this library.
	if _, err := tx.Exec(`DELETE FROM chunk_meta WHERE library_id = ?`, libraryID); err != nil {
		return fmt.Errorf("delete old chunk_meta: %w", err)
	}

	// Insert new chunks.
	insertChunk := `INSERT INTO chunks (library_id, path, breadcrumb, content) VALUES (?, ?, ?, ?)`
	insertMeta := `INSERT INTO chunk_meta (rowid, library_id, path, breadcrumb, byte_size) VALUES (last_insert_rowid(), ?, ?, ?, ?)`

	for _, c := range chunks {
		if _, err := tx.Exec(insertChunk, libraryID, c.Path, c.Breadcrumb, c.Content); err != nil {
			return fmt.Errorf("insert chunk %q: %w", c.Path, err)
		}
		if _, err := tx.Exec(insertMeta, libraryID, c.Path, c.Breadcrumb, len(c.Content)); err != nil {
			return fmt.Errorf("insert chunk_meta %q: %w", c.Path, err)
		}
	}

	return tx.Commit()
}

// Search performs an FTS5 MATCH query with bm25 weights and returns results
// within the token budget (approximated as len(content)/4 tokens).
func (s *Store) Search(libraryID int64, query string, maxTokens int) ([]SearchResult, error) {
	const q = `
		SELECT path, breadcrumb, content, -bm25(chunks, 0.0, 1.5, 2.0, 1.0) AS score
		FROM chunks
		WHERE library_id = ? AND chunks MATCH ?
		ORDER BY score DESC`

	rows, err := s.db.Query(q, libraryID, query)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

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

// Ready returns true if the database is accessible and at least one library
// has been indexed.
func (s *Store) Ready() bool {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM libraries`).Scan(&count)
	return err == nil && count > 0
}

// FindLibraries returns libraries whose name matches the given query (LIKE %query%).
func (s *Store) FindLibraries(query string) ([]Library, error) {
	const q = `SELECT id, name, repo, ref, commit_sha, fetched_at FROM libraries WHERE name LIKE ? ORDER BY name`
	rows, err := s.db.Query(q, "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("find libraries: %w", err)
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var lib Library
		var fetchedAt int64
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Repo, &lib.Ref, &lib.CommitSHA, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan library row: %w", err)
		}
		lib.FetchedAt = time.Unix(fetchedAt, 0).UTC()
		libs = append(libs, lib)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find libraries rows: %w", err)
	}
	return libs, nil
}
