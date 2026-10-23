package domain

// Usage is what an execution consumed, summed over the sessions it
// spanned, as the runtime's result reports it. A runtime that reports no
// usage leaves every field zero.
type Usage struct {
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"promptTokens"`
	CachedTokens     int     `json:"cachedTokens"`
	CompletionTokens int     `json:"completionTokens"`
	ReasoningTokens  int     `json:"reasoningTokens"`
	ToolCalls        int     `json:"toolCalls"`
	CostUSD          float64 `json:"costUsd"`
}

// Validate rejects negative counts.
func (u Usage) Validate() error {
	switch {
	case u.Requests < 0:
		return invalid("usage requests", "must not be negative")
	case u.PromptTokens < 0:
		return invalid("usage prompt tokens", "must not be negative")
	case u.CachedTokens < 0:
		return invalid("usage cached tokens", "must not be negative")
	case u.CompletionTokens < 0:
		return invalid("usage completion tokens", "must not be negative")
	case u.ReasoningTokens < 0:
		return invalid("usage reasoning tokens", "must not be negative")
	case u.ToolCalls < 0:
		return invalid("usage tool calls", "must not be negative")
	case u.CostUSD < 0:
		return invalid("usage cost", "must not be negative")
	}
	return nil
}
