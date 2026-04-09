package index_test

import (
	"path/filepath"
	"testing"

	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/parser"
)

func openTestDB(t *testing.T) *index.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Insert a test source
	err = db.InsertSource(&index.SourceRecord{
		ID:   "test-source",
		Name: "test-source",
	})
	if err != nil {
		t.Fatalf("failed to insert source: %v", err)
	}
	return db
}

func TestSymbolIndexWrite(t *testing.T) {
	db := openTestDB(t)

	sym := &parser.Symbol{
		Name:        "ZodString.min",
		Kind:        parser.SymbolMethod,
		FilePath:    "src/types.ts",
		StartLine:   42,
		EndLine:     55,
		Parameters:  "(length: number)",
		ReturnType:  "ZodString",
		ParentScope: "ZodString",
		Fingerprint: "abc123",
		SourceID:    "test-source",
	}

	id, err := db.InsertSymbol(sym)
	if err != nil {
		t.Fatalf("failed to insert symbol: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero symbol ID")
	}

	// Look it up
	got, err := db.LookupSymbol("test-source", "ZodString.min")
	if err != nil {
		t.Fatalf("failed to lookup symbol: %v", err)
	}
	if got.Name != sym.Name {
		t.Errorf("name: got %q, want %q", got.Name, sym.Name)
	}
	if got.Kind != sym.Kind {
		t.Errorf("kind: got %q, want %q", got.Kind, sym.Kind)
	}
	if got.StartLine != sym.StartLine || got.EndLine != sym.EndLine {
		t.Errorf("lines: got %d-%d, want %d-%d", got.StartLine, got.EndLine, sym.StartLine, sym.EndLine)
	}
}

func TestSearchSymbols(t *testing.T) {
	db := openTestDB(t)

	symbols := []*parser.Symbol{
		{Name: "ZodString.min", Kind: parser.SymbolMethod, FilePath: "a.ts", StartLine: 1, EndLine: 5, SourceID: "test-source"},
		{Name: "ZodString.max", Kind: parser.SymbolMethod, FilePath: "a.ts", StartLine: 6, EndLine: 10, SourceID: "test-source"},
		{Name: "ZodNumber.int", Kind: parser.SymbolMethod, FilePath: "b.ts", StartLine: 1, EndLine: 5, SourceID: "test-source"},
	}
	for _, s := range symbols {
		if _, err := db.InsertSymbol(s); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	results, err := db.SearchSymbols("test-source", "ZodString")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}
