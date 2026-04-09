package mcp

import "context"

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolResult is the response from executing an MCP tool.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a single block of content in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Server is the MCP protocol server (JSON-RPC 2.0 over stdio).
type Server interface {
	// ListTools returns all available MCP tools.
	ListTools(ctx context.Context) []Tool

	// CallTool executes a tool by name with the given arguments.
	CallTool(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error)

	// Serve starts the server loop reading from stdin and writing to stdout.
	Serve(ctx context.Context) error
}
