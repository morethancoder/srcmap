package agent_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morethancoder/srcmap/internal/agent"
	"github.com/morethancoder/srcmap/internal/index"
	"github.com/morethancoder/srcmap/internal/mcp"
	"github.com/morethancoder/srcmap/internal/parser"
)

func TestOpenRouterModelFilter(t *testing.T) {
	// Mock /models response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"id": "small-model", "name": "Small", "context_length": 4000, "pricing": map[string]string{"prompt": "0.000001", "completion": "0.000002"}},
				{"id": "big-model", "name": "Big", "context_length": 128000, "pricing": map[string]string{"prompt": "0.000003", "completion": "0.000006"}},
				{"id": "medium-model", "name": "Medium", "context_length": 32000, "pricing": map[string]string{"prompt": "0.000002", "completion": "0.000004"}},
			},
		})
	}))
	defer srv.Close()

	client := &agent.OpenRouterClient{
		APIKey:  "test",
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}

	models, err := client.ListModels(context.Background(), 32000)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}

	// Should only include models with >= 32k context
	for _, m := range models {
		if m.ContextLength < 32000 {
			t.Errorf("model %q has context %d, should be filtered out", m.ID, m.ContextLength)
		}
	}

	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestCostCalculation(t *testing.T) {
	// $3/1M input, $15/1M output
	ct := agent.NewCostTracker(3.0, 15.0)

	ct.Record(1000, 500)

	last := ct.LastResponse()
	expectedCost := float64(1000)/1_000_000*3.0 + float64(500)/1_000_000*15.0
	if math.Abs(last.CostUSD-expectedCost) > 0.0001 {
		t.Errorf("cost: got %.6f, want %.6f", last.CostUSD, expectedCost)
	}

	ct.Record(2000, 1000)

	session := ct.Session()
	expectedSession := float64(3000)/1_000_000*3.0 + float64(1500)/1_000_000*15.0
	if math.Abs(session.CostUSD-expectedSession) > 0.0001 {
		t.Errorf("session cost: got %.6f, want %.6f", session.CostUSD, expectedSession)
	}
}

func TestSlashClear(t *testing.T) {
	ct := agent.NewCostTracker(3.0, 15.0)
	ct.Record(1000, 500)

	ct.Reset()

	session := ct.Session()
	if session.CostUSD != 0 {
		t.Errorf("expected zero cost after reset, got %.6f", session.CostUSD)
	}
	if session.InputTokens != 0 {
		t.Errorf("expected zero input tokens, got %d", session.InputTokens)
	}
}

func TestCostFooterFormat(t *testing.T) {
	ct := agent.NewCostTracker(3.0, 15.0)
	ct.Record(1243, 891)

	footer := ct.FormatFooter()
	if !strings.Contains(footer, "1243") {
		t.Error("footer should contain input token count")
	}
	if !strings.Contains(footer, "891") {
		t.Error("footer should contain output token count")
	}
	if !strings.Contains(footer, "$") {
		t.Error("footer should contain cost")
	}
}

func TestToolUseLoop(t *testing.T) {
	// Mock OpenRouter: first response has a tool call, second is final text
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Return a tool call
			json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role": "assistant",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]string{
										"name":      "srcmap_source_info",
										"arguments": `{"source":"test"}`,
									},
								},
							},
						},
					},
				},
				"usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20},
			})
		} else {
			// Return final text
			json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "The test source is at version 1.0.",
						},
					},
				},
				"usage": map[string]int{"prompt_tokens": 200, "completion_tokens": 30},
			})
		}
	}))
	defer srv.Close()

	// Set up test DB and handler
	dir := t.TempDir()
	db, _ := index.Open(filepath.Join(dir, "test.db"))
	defer db.Close()
	db.InsertSource(&index.SourceRecord{ID: "test", Name: "test", Version: "1.0"})
	db.InsertSymbol(&parser.Symbol{Name: "Func", Kind: parser.SymbolFunction, FilePath: "f.go", StartLine: 1, EndLine: 5, SourceID: "test"})

	handler := mcp.NewToolHandler(db, dir)
	client := &agent.OpenRouterClient{APIKey: "test", BaseURL: srv.URL, Client: srv.Client()}
	ct := agent.NewCostTracker(3.0, 15.0)
	loop := agent.NewToolLoop(client, handler, "test-model", ct)

	response, err := loop.SendMessage(context.Background(), "Tell me about test source")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if !strings.Contains(response, "version 1.0") {
		t.Errorf("response should contain version info, got: %s", response)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (tool + final), got %d", callCount)
	}

	session := ct.Session()
	if session.InputTokens != 300 {
		t.Errorf("expected 300 input tokens, got %d", session.InputTokens)
	}
}

func TestMultiTurnContext(t *testing.T) {
	callCount := 0
	var lastMessages int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req agent.ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		lastMessages = len(req.Messages)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"role": "assistant", "content": "response"}},
			},
			"usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 10},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, _ := index.Open(filepath.Join(dir, "test.db"))
	defer db.Close()
	db.InsertSource(&index.SourceRecord{ID: "test", Name: "test"})

	handler := mcp.NewToolHandler(db, dir)
	client := &agent.OpenRouterClient{APIKey: "test", BaseURL: srv.URL, Client: srv.Client()}
	ct := agent.NewCostTracker(3.0, 15.0)
	loop := agent.NewToolLoop(client, handler, "test-model", ct)

	loop.SendMessage(context.Background(), "message 1")
	loop.SendMessage(context.Background(), "message 2")
	loop.SendMessage(context.Background(), "message 3")

	// 3rd call sends: system + user1 + assistant1 + user2 + assistant2 + user3 = 6
	if lastMessages != 6 {
		t.Errorf("expected 6 messages in history on 3rd call, got %d", lastMessages)
	}
}
