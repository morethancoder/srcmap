package mcp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/mcp"
	"github.com/morethancoder/srcmap/internal/parser"
)

func setupStdioServer(t *testing.T, input string) (*bytes.Buffer, *mcp.StdioServer) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	db.InsertSource(&index.SourceRecord{
		ID: "test", Name: "test", Version: "1.0",
	})
	db.InsertSymbol(&parser.Symbol{
		Name: "MyFunc", Kind: parser.SymbolFunction,
		FilePath: "test.go", StartLine: 1, EndLine: 10,
		SourceID: "test",
	})

	handler := mcp.NewToolHandler(db, dir)
	reader := strings.NewReader(input)
	var output bytes.Buffer
	server := mcp.NewStdioServer(handler, reader, &output)
	return &output, server
}

func jsonLine(method string, id int, params interface{}) string {
	p, _ := json.Marshal(params)
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  json.RawMessage(p),
	}
	b, _ := json.Marshal(req)
	return string(b)
}

func TestMCPProtocolInitialize(t *testing.T) {
	input := jsonLine("initialize", 1, map[string]interface{}{}) + "\n"
	output, server := setupStdioServer(t, input)

	server.Serve(t.Context())

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (output: %s)", err, output.String())
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Error("wrong protocol version")
	}
}

func TestMCPProtocolToolsList(t *testing.T) {
	input := jsonLine("tools/list", 1, map[string]interface{}{}) + "\n"
	output, server := setupStdioServer(t, input)

	server.Serve(t.Context())

	var resp mcp.JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultJSON, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultJSON), "srcmap_lookup") {
		t.Error("tools list should contain srcmap_lookup")
	}
}

func TestMCPProtocolToolsCall(t *testing.T) {
	params := map[string]interface{}{
		"name": "srcmap_lookup",
		"arguments": map[string]interface{}{
			"source": "test",
			"symbol": "MyFunc",
		},
	}
	input := jsonLine("tools/call", 1, params) + "\n"
	output, server := setupStdioServer(t, input)

	server.Serve(t.Context())

	var resp mcp.JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultJSON, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultJSON), "MyFunc") {
		t.Error("result should contain MyFunc")
	}
}

func TestMCPProtocolUnknownMethod(t *testing.T) {
	input := jsonLine("unknown/method", 1, map[string]interface{}{}) + "\n"
	output, server := setupStdioServer(t, input)

	server.Serve(t.Context())

	var resp mcp.JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error != nil && resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
}

func TestMCPConcurrency(t *testing.T) {
	var lines []string
	for i := 1; i <= 20; i++ {
		params := map[string]interface{}{
			"name": "srcmap_source_info",
			"arguments": map[string]interface{}{
				"source": "test",
			},
		}
		lines = append(lines, jsonLine("tools/call", i, params))
	}
	input := strings.Join(lines, "\n") + "\n"
	output, server := setupStdioServer(t, input)

	server.Serve(t.Context())

	outputLines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(outputLines) != 20 {
		t.Errorf("expected 20 responses, got %d", len(outputLines))
	}
}

func TestMCPInstall(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	os.MkdirAll(claudeDir, 0o755)

	// Verify DetectTarget doesn't panic
	target := mcp.DetectTarget()
	_ = target
}

func TestMCPMultipleRequests(t *testing.T) {
	var lines []string
	lines = append(lines, jsonLine("initialize", 1, map[string]interface{}{}))
	lines = append(lines, jsonLine("tools/list", 2, map[string]interface{}{}))
	params := map[string]interface{}{
		"name":      "srcmap_lookup",
		"arguments": map[string]interface{}{"source": "test", "symbol": "MyFunc"},
	}
	lines = append(lines, jsonLine("tools/call", 3, params))
	input := strings.Join(lines, "\n") + "\n"

	output, server := setupStdioServer(t, input)
	server.Serve(t.Context())

	outputLines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(outputLines) != 3 {
		t.Errorf("expected 3 responses, got %d", len(outputLines))
	}
}
