package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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
			InputPrice:     inputPrice * 1_000_000, // convert per-token to per-1M
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
