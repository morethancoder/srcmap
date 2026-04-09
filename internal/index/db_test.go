package index_test

import (
	"path/filepath"
	"testing"

	"github.com/morethancoder/srcmap/internal/index"
)

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
