package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/internal/fetcher"
	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/mcp"
	"github.com/morethancoder/srcmap/internal/parser"
	"github.com/morethancoder/srcmap/internal/updater"
	"github.com/morethancoder/srcmap/pkg/fileformat"
)

func TestFullFetchAndLookup(t *testing.T) {
	// Mock npm registry
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"repository": map[string]string{"type": "git", "url": "https://github.com/test/pkg.git"},
			"dist-tags":  map[string]string{"latest": "1.0.0"},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".srcmap", "index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Create a mock source directory with a Go file
	sourceDir := filepath.Join(dir, ".srcmap", "sources", "testpkg@1.0.0")
	os.MkdirAll(sourceDir, 0o755)
	os.WriteFile(filepath.Join(sourceDir, "main.go"), []byte(`package main

func Hello() string {
	return "hello"
}

type Config struct {
	Name string
}
`), 0o644)

	// Register source
	db.InsertSource(&index.SourceRecord{ID: "testpkg", Name: "testpkg", Version: "1.0.0", Path: sourceDir})

	// Parse and index
	reg := parser.NewRegistry()
	symbols, err := reg.ParseDirectory(sourceDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for i := range symbols {
		symbols[i].SourceID = "testpkg"
		db.InsertSymbol(&symbols[i])
	}

	// Lookup via MCP tool
	handler := mcp.NewToolHandler(db, filepath.Join(dir, ".srcmap"))
	result, err := handler.CallTool(context.Background(), "srcmap_lookup", map[string]interface{}{
		"source": "testpkg",
		"symbol": "Hello",
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if result.IsError {
		t.Fatalf("lookup error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Hello") {
		t.Error("lookup result should contain Hello")
	}
}

func TestFullDocAddAndQuery(t *testing.T) {
	dir := t.TempDir()
	srcmapDir := filepath.Join(dir, ".srcmap")
	dbPath := filepath.Join(srcmapDir, "index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	db.InsertSource(&index.SourceRecord{ID: "mylib", Name: "mylib", Version: "2.0"})

	// Create doc hierarchy
	hb := fileformat.NewHierarchyBuilder(srcmapDir, "mylib")
	hb.EnsureStructure()
	hb.WriteIndex([]fileformat.SectionSummary{{Name: "core", Description: "Core methods"}})
	hb.WriteSection("core", []fileformat.MethodSummary{
		{Name: "init", Summary: "Initialize the library"},
		{Name: "destroy", Summary: "Clean up resources"},
	})
	hb.WriteMethod("core", &fileformat.DocFile{
		Frontmatter: fileformat.Frontmatter{ID: "mylib.init", Kind: fileformat.KindMethod, Symbol: "init"},
		Body:        "\n# init\n\nInitialize the library with options.\n",
	})

	// Query via MCP tools
	handler := mcp.NewToolHandler(db, srcmapDir)

	// Doc map
	result, _ := handler.CallTool(context.Background(), "srcmap_doc_map", map[string]interface{}{"source": "mylib"})
	if result.IsError {
		t.Fatalf("doc_map error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "core") {
		t.Error("doc map should contain 'core' section")
	}

	// Doc lookup
	result, _ = handler.CallTool(context.Background(), "srcmap_doc_lookup", map[string]interface{}{
		"source": "mylib", "method": "init",
	})
	if result.IsError {
		t.Fatalf("doc_lookup error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Initialize the library") {
		t.Error("doc lookup should contain method content")
	}

	// Doc search
	result, _ = handler.CallTool(context.Background(), "srcmap_doc_search", map[string]interface{}{
		"source": "mylib", "query": "options",
	})
	if result.IsError {
		t.Fatalf("doc_search error: %s", result.Content[0].Text)
	}
}

func TestDiscoveryPreferredOverScrape(t *testing.T) {
	// Simulate: discovery finds a single-file markdown doc
	docContent := "# Data Star\n\n## Installation\n\nInstall via npm.\n\n## Usage\n\nImport and use."

	docSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "text/markdown")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Write([]byte(docContent))
	}))
	defer docSrv.Close()

	// Discovery validates URL
	ds := docfetcher.NewDiscoveryService()
	ds.Client = docSrv.Client()
	result, err := ds.ValidateAndClassify(context.Background(), docSrv.URL+"/docs.md", "")
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if !result.Found {
		t.Fatal("expected Found=true")
	}
	if result.ContentType != docfetcher.ContentSingleMarkdown {
		t.Errorf("expected single-markdown, got %s", result.ContentType)
	}

	// Fetch single file
	fetcher := &docfetcher.SingleFileFetcher{Client: docSrv.Client()}
	page, err := fetcher.Fetch(context.Background(), docSrv.URL+"/docs.md")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Chunk with origin
	chunker := &docfetcher.DefaultChunker{}
	chunks, err := chunker.ChunkWithOrigin("data-star", docSrv.URL+"/docs.md", []docfetcher.RawPage{*page})
	if err != nil {
		t.Fatalf("chunk: %v", err)
	}

	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	if !strings.Contains(chunks[0].Content, "[Origin:") {
		t.Error("chunks should have [Origin] header for discovered source")
	}
}

func TestDiscoveryFallbackToScrape(t *testing.T) {
	ds := docfetcher.NewDiscoveryService()
	result, _ := ds.ValidateAndClassify(context.Background(), "none", "https://example.com")

	if result.Found {
		t.Error("expected not found")
	}
	if result.ContentType != docfetcher.ContentScrape {
		t.Errorf("expected scrape, got %s", result.ContentType)
	}
	if result.FallbackURL != "https://example.com" {
		t.Error("expected fallback URL")
	}
}

func TestFullUpdateCycle(t *testing.T) {
	// Set up stored fingerprints
	store := updater.NewFingerprintStore()
	store.Set("methods/sendMessage.md", "hash1")
	store.Set("methods/editMessage.md", "hash2")
	store.Set("methods/deleteMessage.md", "hash3")

	// Simulate update: sendMessage changed, forwardMessage is new, deleteMessage removed
	current := map[string]string{
		"methods/sendMessage.md":    "hash1_changed",
		"methods/editMessage.md":    "hash2", // unchanged
		"methods/forwardMessage.md": "hash4", // new
	}

	diff := updater.ComputeDiff(store, current)

	if len(diff.New) != 1 || diff.New[0] != "methods/forwardMessage.md" {
		t.Errorf("new: %v", diff.New)
	}
	if len(diff.Changed) != 1 || diff.Changed[0] != "methods/sendMessage.md" {
		t.Errorf("changed: %v", diff.Changed)
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "methods/deleteMessage.md" {
		t.Errorf("removed: %v", diff.Removed)
	}

	// Format changelog
	entry := updater.FormatDiffLayerEntry(diff, "files")
	if !strings.Contains(entry, "Added 1") {
		t.Error("changelog should mention additions")
	}
	if !strings.Contains(entry, "Updated 1") {
		t.Error("changelog should mention updates")
	}
	if !strings.Contains(entry, "Removed 1") {
		t.Error("changelog should mention removals")
	}
}

func TestMCPServerWithRealIndex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.InsertSource(&index.SourceRecord{
		ID: "zod", Name: "zod", Version: "3.22.4",
		MethodCount: 50, SectionCount: 5,
	})
	db.InsertSymbol(&parser.Symbol{
		Name: "ZodString.min", Kind: parser.SymbolMethod,
		FilePath: "src/types.ts", StartLine: 42, EndLine: 55,
		Parameters: "(length: number)", ReturnType: "ZodString",
		ParentScope: "ZodString", SourceID: "zod",
	})
	db.InsertSymbol(&parser.Symbol{
		Name: "ZodString.max", Kind: parser.SymbolMethod,
		FilePath: "src/types.ts", StartLine: 57, EndLine: 70,
		SourceID: "zod",
	})

	handler := mcp.NewToolHandler(db, dir)
	ctx := context.Background()

	// Test all tool types
	tests := []struct {
		name string
		tool string
		args map[string]interface{}
		want string
	}{
		{"lookup", "srcmap_lookup", map[string]interface{}{"source": "zod", "symbol": "ZodString.min"}, "ZodString.min"},
		{"search", "srcmap_search_code", map[string]interface{}{"source": "zod", "query": "ZodString"}, "ZodString"},
		{"source_info", "srcmap_source_info", map[string]interface{}{"source": "zod"}, "3.22.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler.CallTool(ctx, tt.tool, tt.args)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if result.IsError {
				t.Fatalf("tool error: %s", result.Content[0].Text)
			}
			if !strings.Contains(result.Content[0].Text, tt.want) {
				t.Errorf("result should contain %q, got: %s", tt.want, result.Content[0].Text)
			}
		})
	}
}

func TestGlobalLocalResolution(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Insert both global and local sources
	db.InsertSource(&index.SourceRecord{ID: "global-lib", Name: "global-lib", Version: "1.0", Global: true})
	db.InsertSource(&index.SourceRecord{ID: "local-lib", Name: "local-lib", Version: "2.0", Global: false})

	// List all sources
	all, err := db.ListSources(false)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 sources, got %d", len(all))
	}

	// List only global
	globalOnly, err := db.ListSources(true)
	if err != nil {
		t.Fatalf("list global: %v", err)
	}
	if len(globalOnly) != 1 {
		t.Errorf("expected 1 global source, got %d", len(globalOnly))
	}
	if globalOnly[0].ID != "global-lib" {
		t.Errorf("expected global-lib, got %s", globalOnly[0].ID)
	}
}

func TestParsePackageNameIntegration(t *testing.T) {
	tests := []struct {
		input    string
		wantType fetcher.PackageType
		wantName string
	}{
		{"zod", fetcher.PackageNPM, "zod"},
		{"pypi:requests", fetcher.PackagePyPI, "requests"},
		{"github.com/spf13/cobra", fetcher.PackageGoMod, "github.com/spf13/cobra"},
		{"owner/repo", fetcher.PackageGitHub, "owner/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			req := fetcher.ParsePackageName(tt.input, false)
			if req.Type != tt.wantType {
				t.Errorf("type: got %q, want %q", req.Type, tt.wantType)
			}
			if req.Name != tt.wantName {
				t.Errorf("name: got %q, want %q", req.Name, tt.wantName)
			}
		})
	}
}
