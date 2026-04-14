package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"

	"github.com/morethancoder/srcmap/internal/mcp"
)

// StreamEvent describes what's happening during a response.
type StreamEvent struct {
	Type string // "thinking", "text", "tool_call", "tool_result", "done"

	// For "text" and "thinking"
	Delta string

	// For "tool_call"
	ToolName string
	ToolArgs string

	// For "tool_result"
	ToolResult string

	// For "done"
	InputTokens  int
	OutputTokens int
}

// ToolLoop runs the agent tool-use loop with streaming via langchaingo.
type ToolLoop struct {
	LLM         llms.Model
	ToolHandler *mcp.ToolHandler
	ModelID     string
	History     []llms.MessageContent
	CostTracker *CostTracker
	Client      *OpenRouterClient // kept for model listing
}

// NewToolLoop creates a new tool-use loop backed by langchaingo.
func NewToolLoop(client *OpenRouterClient, handler *mcp.ToolHandler, modelID string, costTracker *CostTracker) *ToolLoop {
	llm, _ := openai.New(
		openai.WithToken(client.APIKey),
		openai.WithModel(modelID),
		openai.WithBaseURL(client.BaseURL),
	)

	return &ToolLoop{
		LLM:         llm,
		Client:      client,
		ToolHandler: handler,
		ModelID:     modelID,
		CostTracker: costTracker,
		History: []llms.MessageContent{
			{
				Role: llms.ChatMessageTypeSystem,
				Parts: []llms.ContentPart{
					llms.TextContent{Text: `You are srcmap agent — a terminal assistant that helps users navigate and explore their indexed code sources and documentation.

Your primary job:
- Help users explore sources they've already indexed (use srcmap_list_sources, srcmap_lookup, srcmap_search_code, srcmap_doc_map, etc.)
- Answer questions about libraries using the indexed docs and symbols
- When a user asks about a source that isn't indexed yet, tell them it's not available and offer to fetch it if they want

Only fetch (srcmap_fetch) or add docs (srcmap_docs_add) when the user explicitly asks you to. Never proactively fetch or suggest fetching unless the user requests it.

Keep responses concise and focused on the code/docs data you find.`},
				},
			},
		},
	}
}

// SetModel recreates the LLM client with a new model.
func (tl *ToolLoop) SetModel(modelID string) {
	tl.ModelID = modelID
	llm, _ := openai.New(
		openai.WithToken(tl.Client.APIKey),
		openai.WithModel(modelID),
		openai.WithBaseURL(tl.Client.BaseURL),
	)
	tl.LLM = llm
}

// SendMessage sends a user message and streams events via onEvent callback.
// Returns the final complete text response.
func (tl *ToolLoop) SendMessage(ctx context.Context, userMessage string, onEvent func(StreamEvent)) (string, error) {
	tl.History = append(tl.History, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: userMessage}},
	})

	tools := tl.buildLangchainTools()

	for iterations := 0; iterations < 20; iterations++ {
		// First pass: non-streaming to check if this is a tool call turn
		resp, err := tl.LLM.GenerateContent(ctx, tl.History, llms.WithTools(tools))
		if err != nil {
			return "", fmt.Errorf("chat completion: %w", err)
		}

		tl.recordUsage(resp)

		if resp == nil || len(resp.Choices) == 0 {
			return "", nil
		}

		choice := resp.Choices[0]

		// Emit reasoning/thinking content
		if choice.ReasoningContent != "" && onEvent != nil {
			onEvent(StreamEvent{Type: "thinking", Delta: choice.ReasoningContent})
		}

		// No tool calls — this is a final text response
		if len(choice.ToolCalls) == 0 {
			finalText := choice.Content

			// If we have a callback and got text, re-do the call with streaming
			// to stream the actual text tokens to the terminal
			if onEvent != nil && finalText != "" {
				// We already have the response, stream it character by character
				// from the complete text (simulated streaming for consistency)
				streamText(finalText, onEvent)
			}

			tl.History = append(tl.History, llms.MessageContent{
				Role:  llms.ChatMessageTypeAI,
				Parts: []llms.ContentPart{llms.TextContent{Text: finalText}},
			})

			if onEvent != nil {
				onEvent(StreamEvent{
					Type:         "done",
					InputTokens:  tl.CostTracker.LastResponse().InputTokens,
					OutputTokens: tl.CostTracker.LastResponse().OutputTokens,
				})
			}
			return finalText, nil
		}

		// Tool call turn — show tool calls, suppress any raw text junk
		var aiParts []llms.ContentPart
		if choice.Content != "" {
			// Only add real text content, not JSON fragments
			trimmed := strings.TrimSpace(choice.Content)
			if trimmed != "" && !strings.HasPrefix(trimmed, "[{") && !strings.HasPrefix(trimmed, "{\"") {
				aiParts = append(aiParts, llms.TextContent{Text: choice.Content})
				if onEvent != nil {
					streamText(choice.Content, onEvent)
				}
			}
		}
		for _, tc := range choice.ToolCalls {
			aiParts = append(aiParts, llms.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				FunctionCall: &llms.FunctionCall{
					Name:      tc.FunctionCall.Name,
					Arguments: tc.FunctionCall.Arguments,
				},
			})
		}
		if len(aiParts) == 0 {
			// Must have at least one part
			aiParts = append(aiParts, llms.TextContent{Text: ""})
		}
		tl.History = append(tl.History, llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: aiParts,
		})

		// Execute each tool call
		for _, tc := range choice.ToolCalls {
			if onEvent != nil {
				onEvent(StreamEvent{
					Type:     "tool_call",
					ToolName: tc.FunctionCall.Name,
					ToolArgs: tc.FunctionCall.Arguments,
				})
			}

			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
				args = make(map[string]interface{})
			}

			result, err := tl.ToolHandler.CallTool(ctx, tc.FunctionCall.Name, args)
			if err != nil {
				result = &mcp.ToolResult{
					Content: []mcp.ContentBlock{{Type: "text", Text: err.Error()}},
					IsError: true,
				}
			}

			var resultText string
			for _, block := range result.Content {
				if block.Text != "" {
					resultText += block.Text
				}
			}

			if onEvent != nil {
				onEvent(StreamEvent{
					Type:       "tool_result",
					ToolName:   tc.FunctionCall.Name,
					ToolResult: resultText,
				})
			}

			tl.History = append(tl.History, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: tc.ID,
						Name:       tc.FunctionCall.Name,
						Content:    resultText,
					},
				},
			})
		}
	}

	return "", fmt.Errorf("too many tool iterations")
}

// ClearHistory resets the conversation history.
func (tl *ToolLoop) ClearHistory() {
	tl.History = tl.History[:1] // keep system message
	tl.CostTracker.Reset()
}

func (tl *ToolLoop) recordUsage(resp *llms.ContentResponse) {
	if resp == nil || len(resp.Choices) == 0 {
		return
	}
	info := resp.Choices[0].GenerationInfo
	inputTokens, _ := getIntFromGenInfo(info, "PromptTokens")
	outputTokens, _ := getIntFromGenInfo(info, "CompletionTokens")
	if inputTokens > 0 || outputTokens > 0 {
		tl.CostTracker.Record(inputTokens, outputTokens)
	}
}

func (tl *ToolLoop) buildLangchainTools() []llms.Tool {
	mcpTools := tl.ToolHandler.AllTools()
	var tools []llms.Tool
	for _, t := range mcpTools {
		tools = append(tools, llms.Tool{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return tools
}

// streamText emits text content word-by-word for a smooth streaming feel.
func streamText(text string, onEvent func(StreamEvent)) {
	// Emit in small chunks for a natural streaming feel
	const chunkSize = 4
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		onEvent(StreamEvent{Type: "text", Delta: string(runes[i:end])})
	}
}

func getIntFromGenInfo(info map[string]any, key string) (int, bool) {
	if info == nil {
		return 0, false
	}
	v, ok := info[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
