package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// OpenRouterClient communicates with the OpenRouter API.
type OpenRouterClient struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewOpenRouterClient creates a client with the given API key.
func NewOpenRouterClient(apiKey string) *OpenRouterClient {
	return &OpenRouterClient{
		APIKey:  apiKey,
		BaseURL: "https://openrouter.ai/api/v1",
		Client:  http.DefaultClient,
	}
}

// Model represents an OpenRouter model.
type Model struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	ContextLength  int     `json:"context_length"`
	InputPrice     float64 // per 1M tokens
	OutputPrice    float64 // per 1M tokens
	ToolUse        bool
	PopularityRank int
}

// ModelListResponse from OpenRouter /models API.
type modelListResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ContextLength int    `json:"context_length"`
		Pricing       struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		TopProvider struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		} `json:"top_provider"`
		Architecture struct {
			Modality string `json:"modality"`
		} `json:"architecture"`
	} `json:"data"`
}

// ListModels fetches available models, filtered for tool use and minimum context.
func (c *OpenRouterClient) ListModels(ctx context.Context, minContext int) ([]Model, error) {
	url := c.BaseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close()

	var result modelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding models: %w", err)
	}

	var models []Model
	for i, m := range result.Data {
		if m.ContextLength < minContext {
			continue
		}
		// Parse pricing
		var inputPrice, outputPrice float64
		fmt.Sscanf(m.Pricing.Prompt, "%f", &inputPrice)
		fmt.Sscanf(m.Pricing.Completion, "%f", &outputPrice)

		models = append(models, Model{
			ID:             m.ID,
			Name:           m.Name,
			ContextLength:  m.ContextLength,
			InputPrice:     inputPrice * 1_000_000,  // convert per-token to per-1M
			OutputPrice:    outputPrice * 1_000_000,
			PopularityRank: i,
		})
	}

	// Sort by popularity × cost efficiency
	sort.Slice(models, func(i, j int) bool {
		scoreI := float64(models[i].PopularityRank) * (models[i].InputPrice + models[i].OutputPrice)
		scoreJ := float64(models[j].PopularityRank) * (models[j].InputPrice + models[j].OutputPrice)
		return scoreI < scoreJ
	})

	return models, nil
}

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	Model    string            `json:"model"`
	Messages []ChatMessage     `json:"messages"`
	Tools    []ChatTool        `json:"tools,omitempty"`
	Stream   bool              `json:"stream"`
}

// ChatMessage represents a message in the conversation.
type ChatMessage struct {
	Role       string          `json:"role"`
	Content    interface{}     `json:"content,omitempty"`  // string or null
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool call from the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function details of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatTool represents a tool definition for the API.
type ChatTool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a function tool.
type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// StreamChunk represents a chunk from streaming response.
type StreamChunk struct {
	Delta     ChatMessage
	Thinking  string // reasoning/thinking tokens
	FinishReason string
	Usage     *Usage
}

// Usage tracks token usage from a response.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// ChatCompletion sends a non-streaming chat request.
func (c *OpenRouterClient) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatMessage, *Usage, error) {
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("chat completion: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message ChatMessage `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, nil, fmt.Errorf("no choices in response")
	}

	return &result.Choices[0].Message, result.Usage, nil
}

// StreamChatCompletion sends a streaming chat request and calls the handler for each chunk.
func (c *OpenRouterClient) StreamChatCompletion(ctx context.Context, req *ChatRequest, handler func(StreamChunk)) (*Usage, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("stream chat: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var usage *Usage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Choices []struct {
				Delta        ChatMessage `json:"delta"`
				FinishReason string      `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Usage != nil {
			usage = event.Usage
		}

		if len(event.Choices) > 0 {
			handler(StreamChunk{
				Delta:        event.Choices[0].Delta,
				FinishReason: event.Choices[0].FinishReason,
				Usage:        event.Usage,
			})
		}
	}

	return usage, scanner.Err()
}
