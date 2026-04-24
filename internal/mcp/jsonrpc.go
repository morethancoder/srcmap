package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// report/reportN are kept as no-op stubs so tool handlers can continue
// calling them without cluttering call sites. Progress notifications were
// removed because the MCP TypeScript client (used by Claude Code) drops
// the transport when it sees a progress notification whose token it no
// longer tracks — and our emissions race against response delivery.
func report(context.Context, string)                    {}
func reportN(context.Context, float64, float64, string) {}

// StdioServer implements the MCP protocol over stdin/stdout.
type StdioServer struct {
	handler *ToolHandler
	reader  io.Reader
	writer  io.Writer
	mu      sync.Mutex
}

// NewStdioServer creates a new MCP server reading from r and writing to w.
func NewStdioServer(handler *ToolHandler, r io.Reader, w io.Writer) *StdioServer {
	return &StdioServer{
		handler: handler,
		reader:  r,
		writer:  w,
	}
}

// Serve runs the server loop, reading JSON-RPC requests and writing responses.
func (s *StdioServer) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.reader)
	const maxMessage = 16 * 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessage)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(nil, -32700, "Parse error")
			continue
		}

		resp := s.safeHandle(ctx, &req)
		if resp != nil {
			s.writeResponse(resp)
		}
	}

	return scanner.Err()
}

// safeHandle wraps handleRequest with panic recovery so a buggy tool
// doesn't tear down the stdio server.
func (s *StdioServer) safeHandle(ctx context.Context, req *JSONRPCRequest) (resp *JSONRPCResponse) {
	defer func() {
		if r := recover(); r != nil {
			if len(req.ID) == 0 || string(req.ID) == "null" {
				resp = nil
				return
			}
			resp = &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32603, Message: fmt.Sprintf("internal error: %v", r)},
			}
		}
	}()
	return s.handleRequest(ctx, req)
}

func (s *StdioServer) handleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "ping":
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}}
	}

	if isNotification || strings.HasPrefix(req.Method, "notifications/") {
		return nil
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Error:   &JSONRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
	}
}

// initializeParams captures the client's requested protocol version so the
// server can echo it back when compatible.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// supportedProtocolVersions lists MCP versions this server can speak, newest first.
var supportedProtocolVersions = []string{"2025-03-26", "2024-11-05"}

func (s *StdioServer) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	var params initializeParams
	_ = json.Unmarshal(req.Params, &params)

	version := supportedProtocolVersions[len(supportedProtocolVersions)-1]
	for _, v := range supportedProtocolVersions {
		if v == params.ProtocolVersion {
			version = v
			break
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": version,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "srcmap",
				"version": "0.1.0",
			},
		},
	}
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

func (s *StdioServer) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  toolsListResult{Tools: s.handler.AllTools()},
	}
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func (s *StdioServer) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	result, err := s.handler.CallTool(ctx, params.Name, params.Arguments)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32603, Message: err.Error()},
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *StdioServer) writeResponse(resp *JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	fmt.Fprintf(s.writer, "%s\n", data)
}

func (s *StdioServer) writeError(id json.RawMessage, code int, message string) {
	s.writeResponse(&JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
	})
}
