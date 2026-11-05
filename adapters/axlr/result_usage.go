package axlr

import "github.com/underpass-ai/axlr-bots/domain"

// resultUsage is the usage ledger of the spanned sessions, summed.
type resultUsage struct {
	Requests          int     `json:"requests"`
	PromptTokens      int     `json:"prompt_tokens"`
	CachedTokens      int     `json:"cached_tokens"`
	CacheWriteTokens  int     `json:"cache_write_tokens"`
	CompletionTokens  int     `json:"completion_tokens"`
	ReasoningTokens   int     `json:"reasoning_tokens"`
	CostUSD           float64 `json:"cost_usd"`
	CostRequests      int     `json:"cost_requests"`
	UnmeteredRequests int     `json:"unmetered_requests"`
}

// usage is the ledger as the domain counts it, with the tool calls the
// run is known to have made.
func (u resultUsage) usage(toolCalls int) domain.Usage {
	return domain.Usage{
		Requests:         u.Requests,
		PromptTokens:     u.PromptTokens,
		CachedTokens:     u.CachedTokens,
		CompletionTokens: u.CompletionTokens,
		ReasoningTokens:  u.ReasoningTokens,
		ToolCalls:        toolCalls,
		CostUSD:          u.CostUSD,
	}
}
