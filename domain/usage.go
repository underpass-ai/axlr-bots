package domain

// Usage is what an execution consumed. The interim runtime contract
// reports none of it, so every field stays zero until the result object
// carries usage per request and cost.
type Usage struct {
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
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
	case u.CompletionTokens < 0:
		return invalid("usage completion tokens", "must not be negative")
	case u.ToolCalls < 0:
		return invalid("usage tool calls", "must not be negative")
	case u.CostUSD < 0:
		return invalid("usage cost", "must not be negative")
	}
	return nil
}
