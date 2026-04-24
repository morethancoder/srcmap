package mcp_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morethancoder/srcmap/internal/docfetcher"
	"github.com/morethancoder/srcmap/internal/fetcher"
	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/mcp"
	"github.com/morethancoder/srcmap/internal/parser"
	"github.com/morethancoder/srcmap/pkg/fileformat"
)

func setupTestHandler(t *testing.T) (*mcp.ToolHandler, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Insert test source
	db.InsertSource(&index.SourceRecord{
		ID:          "test-source",
		Name:        "test-source",
		Version:     "1.0.0",
		LastUpdated: "2026-04-10T00:00:00Z",
		MethodCount: 3,
	})

	// Insert test symbol
	db.InsertSymbol(&parser.Symbol{
		Name:       "TestFunc",
		Kind:       parser.SymbolFunction,
		FilePath:   "test.go",
		StartLine:  10,
		EndLine:    20,
		Parameters: "(x int)",
		ReturnType: "string",
		SourceID:   "test-source",
	})

	handler := mcp.NewToolHandler(db, dir)
	return handler, dir
}

func TestMCPToolListing(t *testing.T) {
	handler, _ := setupTestHandler(t)
	tools := handler.AllTools()

	expectedTools := []string{
		"srcmap_lookup", "srcmap_search_code", "srcmap_doc_map",
		"srcmap_doc_section", "srcmap_doc_lookup", "srcmap_doc_concept",
		"srcmap_doc_search", "srcmap_doc_gotchas", "srcmap_source_info",
		"srcmap_process_chunk", "srcmap_process_status",
		"srcmap_find", "srcmap_list_sources", "srcmap_delete_source",
	}

	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name] = true
	}

	for _, want := range expectedTools {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}

	// Fetch / update / outdated only appear when the orchestrator is wired
	// up; the setupTestHandler leaves it nil, so make sure they are NOT
	// advertised in that mode.
	for _, notExpected := range []string{"srcmap_fetch", "srcmap_docs_add", "srcmap_update_source", "srcmap_outdated"} {
		if names[notExpected] {
			t.Errorf("tool %q should not appear when orchestrator is nil", notExpected)
		}
	}
}

func TestMCPLookupTool(t *testing.T) {
	handler, _ := setupTestHandler(t)

	result, err := handler.CallTool(context.Background(), "srcmap_lookup", map[string]interface{}{
		"source": "test-source",
		"symbol": "TestFunc",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "TestFunc") {
		t.Error("result should contain symbol name")
	}
	if !strings.Contains(result.Content[0].Text, "10-20") {
		t.Error("result should contain line range")
	}
}

func TestMCPSourceInfo(t *testing.T) {
	handler, _ := setupTestHandler(t)

	result, err := handler.CallTool(context.Background(), "srcmap_source_info", map[string]interface{}{
		"source": "test-source",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "test-source") {
		t.Error("result should contain source name")
	}
}

func TestMCPDocLookupTool(t *testing.T) {
	handler, dir := setupTestHandler(t)

	// Create a method doc file
	hb := fileformat.NewHierarchyBuilder(dir, "test-source")
	hb.EnsureStructure()
	hb.WriteMethod("api", &fileformat.DocFile{
		Frontmatter: fileformat.Frontmatter{
			ID:     "test-source.myMethod",
			Kind:   fileformat.KindMethod,
			Symbol: "myMethod",
		},
		Body: "\n# myMethod\n\nDoes something useful.\n",
	})

	result, err := handler.CallTool(context.Background(), "srcmap_doc_lookup", map[string]interface{}{
		"source": "test-source",
		"method": "mymethod",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Does something useful") {
		t.Error("result should contain method content")
	}
}

func TestMCPProcessStatus(t *testing.T) {
	handler, _ := setupTestHandler(t)

	// Insert some chunks
	db := handler.DB
	for i := 0; i < 5; i++ {
		status := docfetcher.ChunkPending
		if i < 3 {
			status = docfetcher.ChunkProcessed
		}
		db.InsertChunk(&docfetcher.Chunk{
			SourceID: "test-source",
			Status:   status,
		})
	}

	result, err := handler.CallTool(context.Background(), "srcmap_process_status", map[string]interface{}{
		"source": "test-source",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "Pending: 2") {
		t.Errorf("expected 2 pending, got: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "Processed: 3") {
		t.Errorf("expected 3 processed, got: %s", result.Content[0].Text)
	}
}

func TestMCPErrorHandling(t *testing.T) {
	handler, _ := setupTestHandler(t)

	result, err := handler.CallTool(context.Background(), "srcmap_lookup", map[string]interface{}{
		"source": "nonexistent",
		"symbol": "missing",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing symbol")
	}
}

func TestMCPToolListingWithOrchestrator(t *testing.T) {
	handler, _ := setupTestHandler(t)
	handler.Orchestrator = fetcher.NewOrchestrator("", "")

	names := map[string]bool{}
	for _, tl := range handler.AllTools() {
		names[tl.Name] = true
	}
	for _, want := range []string{"srcmap_fetch", "srcmap_docs_add", "srcmap_ingest_local_docs", "srcmap_update_source", "srcmap_outdated"} {
		if !names[want] {
			t.Errorf("expected tool %q to appear when orchestrator is set", want)
		}
	}
}

func TestMCPUpdateRequiresExistingSource(t *testing.T) {
	handler, _ := setupTestHandler(t)
	handler.Orchestrator = fetcher.NewOrchestrator("", "")

	res, err := handler.CallTool(context.Background(), "srcmap_update_source", map[string]interface{}{
		"source": "does-not-exist",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected error for missing source, got: %s", res.Content[0].Text)
	}
	if !strings.Contains(res.Content[0].Text, "srcmap_fetch") {
		t.Errorf("error should hint at srcmap_fetch; got: %s", res.Content[0].Text)
	}
}

func TestMCPRejectsPathTraversalSource(t *testing.T) {
	handler, _ := setupTestHandler(t)

	for _, bad := range []string{"../etc", "..\\windows", "a/b", "/abs/path", ".."} {
		res, err := handler.CallTool(context.Background(), "srcmap_doc_map", map[string]interface{}{
			"source": bad,
		})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		if !res.IsError {
			t.Errorf("expected error for source %q, got success: %s", bad, res.Content[0].Text)
		}
	}
}

func TestMCPDocSearch(t *testing.T) {
	handler, dir := setupTestHandler(t)

	hb := fileformat.NewHierarchyBuilder(dir, "test-source")
	hb.EnsureStructure()
	hb.WriteMethod("api", &fileformat.DocFile{
		Frontmatter: fileformat.Frontmatter{ID: "sendMessage", Kind: fileformat.KindMethod, Symbol: "sendMessage"},
		Body:        "\n# sendMessage\n\nSend a text message to a chat using the reply_markup parameter.\n",
	})

	result, err := handler.CallTool(context.Background(), "srcmap_doc_search", map[string]interface{}{
		"source": "test-source",
		"query":  "reply_markup",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "sendmessage.md") {
		t.Errorf("expected to find sendmessage.md, got: %s", result.Content[0].Text)
	}
}
