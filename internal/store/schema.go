package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection with all codelens operations.
type DB struct {
	conn *sql.DB
	path string
}

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS chunks (
    id           TEXT PRIMARY KEY,
    file_path    TEXT NOT NULL,
    start_line   INTEGER NOT NULL,
    end_line     INTEGER NOT NULL,
    content      TEXT NOT NULL,
    language     TEXT,
    symbol       TEXT,
    symbol_kind  TEXT,
    hash         TEXT NOT NULL,
    indexed_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_chunks_file     ON chunks(file_path);
CREATE INDEX IF NOT EXISTS idx_chunks_language ON chunks(language);

CREATE TABLE IF NOT EXISTS embeddings (
    chunk_id TEXT PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    vector   BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS memories (
    id         TEXT PRIMARY KEY,
    insight    TEXT NOT NULL,
    memory_type TEXT NOT NULL DEFAULT 'insight',
    status     TEXT NOT NULL DEFAULT 'published',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    published_at DATETIME,
    expires_at DATETIME NOT NULL,
    hit_count  INTEGER DEFAULT 0,
    last_used  DATETIME
);

CREATE TABLE IF NOT EXISTS citations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    memory_id  TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    file_path  TEXT NOT NULL,
    line_start INTEGER NOT NULL,
    line_end   INTEGER NOT NULL,
    hash       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_citations_memory ON citations(memory_id);

CREATE TABLE IF NOT EXISTS memory_proposals (
    id          TEXT PRIMARY KEY,
    insight     TEXT NOT NULL,
    memory_type TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    reason      TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    reviewed_at DATETIME
);

CREATE TABLE IF NOT EXISTS proposal_citations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id TEXT NOT NULL REFERENCES memory_proposals(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    line_start  INTEGER NOT NULL,
    line_end    INTEGER NOT NULL,
    hash        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_proposal_citations_proposal ON proposal_citations(proposal_id);

CREATE TABLE IF NOT EXISTS index_failures (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path   TEXT NOT NULL,
    error       TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_index_failures_file ON index_failures(file_path);

CREATE TABLE IF NOT EXISTS file_hashes (
    file_path  TEXT PRIMARY KEY,
    hash       TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// Open opens (or creates) the SQLite database at the given path.
func Open(dbPath string) (*DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_time_format=sqlite",
		dbPath,
	)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite: single writer

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := applyMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return &DB{conn: conn, path: dbPath}, nil
}

func (db *DB) Close() error { return db.conn.Close() }
func (db *DB) Path() string { return db.path }

func applyMigrations(conn *sql.DB) error {
	changes := []struct {
		table  string
		column string
		sql    string
	}{
		{
			table:  "memories",
			column: "memory_type",
			sql:    `ALTER TABLE memories ADD COLUMN memory_type TEXT NOT NULL DEFAULT 'insight'`,
		},
		{
			table:  "memories",
			column: "status",
			sql:    `ALTER TABLE memories ADD COLUMN status TEXT NOT NULL DEFAULT 'published'`,
		},
		{
			table:  "memories",
			column: "published_at",
			sql:    `ALTER TABLE memories ADD COLUMN published_at DATETIME`,
		},
	}

	for _, c := range changes {
		ok, err := hasColumn(conn, c.table, c.column)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := conn.Exec(c.sql); err != nil {
			return err
		}
	}
	return nil
}

func hasColumn(conn *sql.DB, table, column string) (bool, error) {
	rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
