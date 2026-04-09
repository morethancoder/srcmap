package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sources (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    repo_url    TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL DEFAULT '',
    global      INTEGER NOT NULL DEFAULT 0,
    last_updated TEXT NOT NULL DEFAULT '',
    section_count   INTEGER NOT NULL DEFAULT 0,
    method_count    INTEGER NOT NULL DEFAULT 0,
    concept_count   INTEGER NOT NULL DEFAULT 0,
    gotcha_count    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS symbols (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id    TEXT NOT NULL REFERENCES sources(id),
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    file_path    TEXT NOT NULL,
    start_line   INTEGER NOT NULL,
    end_line     INTEGER NOT NULL,
    parameters   TEXT NOT NULL DEFAULT '',
    return_type  TEXT NOT NULL DEFAULT '',
    parent_scope TEXT NOT NULL DEFAULT '',
    fingerprint  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_symbols_source ON symbols(source_id);
CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);

CREATE TABLE IF NOT EXISTS doc_files (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL REFERENCES sources(id),
    kind        TEXT NOT NULL,
    section     TEXT NOT NULL DEFAULT '',
    file_path   TEXT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    last_updated TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_doc_files_source ON doc_files(source_id);

CREATE TABLE IF NOT EXISTS doc_links (
    symbol_id   INTEGER NOT NULL REFERENCES symbols(id),
    doc_file_id TEXT NOT NULL REFERENCES doc_files(id),
    confidence  REAL NOT NULL DEFAULT 0.0,
    PRIMARY KEY (symbol_id, doc_file_id)
);

CREATE TABLE IF NOT EXISTS fingerprints (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id   TEXT NOT NULL REFERENCES sources(id),
    file_path   TEXT NOT NULL,
    hash        TEXT NOT NULL,
    UNIQUE(source_id, file_path)
);

CREATE TABLE IF NOT EXISTS chunks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id        TEXT NOT NULL REFERENCES sources(id),
    page_url         TEXT NOT NULL DEFAULT '',
    chunk_index      INTEGER NOT NULL DEFAULT 0,
    heading          TEXT NOT NULL DEFAULT '',
    content          TEXT NOT NULL DEFAULT '',
    estimated_tokens INTEGER NOT NULL DEFAULT 0,
    fingerprint      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending'
);

CREATE INDEX IF NOT EXISTS idx_chunks_source ON chunks(source_id);
CREATE INDEX IF NOT EXISTS idx_chunks_status ON chunks(status);
`

// DB wraps the SQLite database connection.
type DB struct {
	conn *sql.DB
}

// Open opens or creates the SQLite database at the given path.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying sql.DB for advanced queries.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	// Check current version
	var version int
	row := db.conn.QueryRow("SELECT version FROM schema_version LIMIT 1")
	err := row.Scan(&version)
	if err != nil {
		// Table doesn't exist yet — run full schema
		if _, err := db.conn.Exec(schema); err != nil {
			return fmt.Errorf("creating schema: %w", err)
		}
		_, err = db.conn.Exec("INSERT INTO schema_version (version) VALUES (?)", schemaVersion)
		return err
	}

	if version >= schemaVersion {
		return nil
	}

	// Future migrations go here
	_, err = db.conn.Exec("UPDATE schema_version SET version = ?", schemaVersion)
	return err
}
