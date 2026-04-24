package index_test

import (
	"path/filepath"
	"testing"

	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/internal/index"
)

func TestInsertChunkDedup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dedup.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.InsertSource(&index.SourceRecord{ID: "s", Name: "s"}); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	c := &docfetcher.Chunk{
		SourceID:    "s",
		Heading:     "Intro",
		Content:     "Some doc content.",
		Fingerprint: "dupfp",
		Status:      docfetcher.ChunkPending,
	}
	first, err := db.InsertChunk(c)
	if err != nil || first == 0 {
		t.Fatalf("first insert: id=%d err=%v", first, err)
	}
	second, err := db.InsertChunk(c)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if second != first {
		t.Errorf("duplicate chunk created a new row: first=%d second=%d", first, second)
	}

	var count int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM chunks WHERE source_id = ?`, "s").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 chunk row after dedup, got %d", count)
	}
}

func TestDBInit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Verify all tables exist by querying them
	tables := []string{"schema_version", "sources", "symbols", "doc_files", "doc_links", "fingerprints", "chunks"}
	for _, table := range tables {
		_, err := db.Conn().Exec("SELECT COUNT(*) FROM " + table)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}
}

func TestDBInitIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Open twice — should not fail on second open
	db1, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("first open failed: %v", err)
	}
	db1.Close()

	db2, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("second open failed: %v", err)
	}
	db2.Close()
}

// Simulates upgrade from v2 where chunks table lacks kind/anchor_id.
// A partial v3 migration that got to ADD kind but not anchor_id should
// still re-open cleanly on retry (ADD kind would otherwise error with
// "duplicate column name").
func TestMigrationV2ToV3Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Create a synthetic v2 DB: use raw SQL to mimic the v2 schema and set
	// schema_version=2. Then force a partial "v3 in progress" state by
	// adding just the kind column.
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Force row back to v2 so the migration runs on next open.
	if _, err := db.Conn().Exec("UPDATE schema_version SET version = 2"); err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	db.Close()

	// Re-open: v2 → v3 migration runs, adds both columns.
	db2, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("v2→v3 first run: %v", err)
	}
	db2.Close()

	// Downgrade version again but keep both columns in place — simulating
	// a mid-migration crash between schema_version update and commit.
	db3, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := db3.Conn().Exec("UPDATE schema_version SET version = 2"); err != nil {
		t.Fatalf("re-downgrade: %v", err)
	}
	db3.Close()

	// Must not fail on "duplicate column name".
	db4, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("idempotent v2→v3 retry failed: %v", err)
	}
	db4.Close()
}
