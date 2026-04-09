package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/morethancoder/srcmap/internal/mcp"
)

// ToolLoop runs the agent tool-use loop: send message, execute tool calls, repeat.
type ToolLoop struct {
	Client      *OpenRouterClient
	ToolHandler *mcp.ToolHandler
	ModelID     string
	History     []ChatMessage
	CostTracker *CostTracker
}

// NewToolLoop creates a new tool-use loop.
func NewToolLoop(client *OpenRouterClient, handler *mcp.ToolHandler, modelID string, costTracker *CostTracker) *ToolLoop {
	return &ToolLoop{
		Client:      client,
		ToolHandler: handler,
		ModelID:     modelID,
		CostTracker: costTracker,
		History: []ChatMessage{
			{
				Role:    "system",
				Content: "You are srcmap agent, a helpful assistant that uses srcmap tools to research dependencies and APIs. Use the available tools to fetch source code, look up symbols, and query documentation.",
			},
		},
	}
}

// SendMessage sends a user message and runs the tool-use loop until a final text response.
func (tl *ToolLoop) SendMessage(ctx context.Context, userMessage string) (string, error) {
	tl.History = append(tl.History, ChatMessage{Role: "user", Content: userMessage})

	tools := tl.buildToolDefs()

	for {
		req := &ChatRequest{
			Model:    tl.ModelID,
			Messages: tl.History,
			Tools:    tools,
		}

		msg, usage, err := tl.Client.ChatCompletion(ctx, req)
		if err != nil {
			return "", fmt.Errorf("chat completion: %w", err)
		}

		if usage != nil {
			tl.CostTracker.Record(usage.PromptTokens, usage.CompletionTokens)
		}

		tl.History = append(tl.History, *msg)

		// If no tool calls, return the text response
		if len(msg.ToolCalls) == 0 {
			text, _ := msg.Content.(string)
			return text, nil
		}

		// Execute tool calls
		for _, tc := range msg.ToolCalls {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = make(map[string]interface{})
			}

			result, err := tl.ToolHandler.CallTool(ctx, tc.Function.Name, args)
			if err != nil {
				result = &mcp.ToolResult{
					Content: []mcp.ContentBlock{{Type: "text", Text: err.Error()}},
					IsError: true,
				}
			}

			// Extract text from result
			var text string
			for _, block := range result.Content {
				if block.Text != "" {
					text += block.Text
				}
			}

			tl.History = append(tl.History, ChatMessage{
				Role:       "tool",
				Content:    text,
				ToolCallID: tc.ID,
			})
		}
	}
}

// ClearHistory resets the conversation history.
func (tl *ToolLoop) ClearHistory() {
	tl.History = tl.History[:1] // keep system message
	tl.CostTracker.Reset()
}

func (tl *ToolLoop) buildToolDefs() []ChatTool {
	mcpTools := tl.ToolHandler.AllTools()
	var tools []ChatTool
	for _, t := range mcpTools {
		tools = append(tools, ChatTool{
			Type: "function",
			Function: ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return tools
}
