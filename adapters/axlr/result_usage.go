package axlr

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
