package agent

import (
	"fmt"
	"sync"
)

// CostTracker tracks token usage and cost across a session.
type CostTracker struct {
	mu             sync.Mutex
	inputPrice     float64 // price per 1M input tokens
	outputPrice    float64 // price per 1M output tokens
	sessionInput   int
	sessionOutput  int
	sessionCost    float64
	lastInput      int
	lastOutput     int
	lastCost       float64
}

// NewCostTracker creates a cost tracker with the given per-1M-token prices.
func NewCostTracker(inputPricePerMillion, outputPricePerMillion float64) *CostTracker {
	return &CostTracker{
		inputPrice:  inputPricePerMillion,
		outputPrice: outputPricePerMillion,
	}
}

// Record records token usage from a response.
func (ct *CostTracker) Record(inputTokens, outputTokens int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	cost := float64(inputTokens)/1_000_000*ct.inputPrice + float64(outputTokens)/1_000_000*ct.outputPrice

	ct.lastInput = inputTokens
	ct.lastOutput = outputTokens
	ct.lastCost = cost

	ct.sessionInput += inputTokens
	ct.sessionOutput += outputTokens
	ct.sessionCost += cost
}

// LastResponse returns the cost info for the last response.
func (ct *CostTracker) LastResponse() CostInfo {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return CostInfo{
		InputTokens:  ct.lastInput,
		OutputTokens: ct.lastOutput,
		CostUSD:      ct.lastCost,
	}
}

// Session returns the cumulative session cost info.
func (ct *CostTracker) Session() CostInfo {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return CostInfo{
		InputTokens:  ct.sessionInput,
		OutputTokens: ct.sessionOutput,
		CostUSD:      ct.sessionCost,
	}
}

// Reset clears all session totals.
func (ct *CostTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.sessionInput = 0
	ct.sessionOutput = 0
	ct.sessionCost = 0
	ct.lastInput = 0
	ct.lastOutput = 0
	ct.lastCost = 0
}

// FormatFooter returns the cost display footer.
func (ct *CostTracker) FormatFooter() string {
	last := ct.LastResponse()
	session := ct.Session()

	return fmt.Sprintf(
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"+
			"  ↑ %d in   ↓ %d out    $%.4f  this response\n"+
			"  Session: %d tokens              $%.4f total\n"+
			"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		last.InputTokens, last.OutputTokens, last.CostUSD,
		session.InputTokens+session.OutputTokens, session.CostUSD,
	)
}
