package agent

import "context"

// Message represents a chat message in the agent conversation.
type Message struct {
	Role    string `json:"role"` // "user", "assistant", "tool"
	Content string `json:"content"`
}

// CostInfo tracks token usage and cost for a response or session.
type CostInfo struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// Agent is the interactive terminal chat interface.
type Agent interface {
	// Run starts the interactive agent loop.
	Run(ctx context.Context) error
}
