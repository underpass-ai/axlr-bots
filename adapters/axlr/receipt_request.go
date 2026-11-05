package axlr

import "time"

// receiptRequest is one model request of the receipt, with what it
// consumed. CostUSD is nil when the request reported no cost.
type receiptRequest struct {
	StartedAt        time.Time `json:"started_at"`
	PromptTokens     int       `json:"prompt_tokens"`
	CachedTokens     int       `json:"cached_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	CostUSD          *float64  `json:"cost_usd"`
}
