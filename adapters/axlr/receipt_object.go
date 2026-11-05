package axlr

import (
	"encoding/json"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// receiptObject is the part of the receipt that tells what each model
// request consumed. Requests is nil when the receipt lists none: a runtime
// older than the itemized receipt.
type receiptObject struct {
	Requests *[]receiptRequest `json:"requests"`
}

// usageSince sums the receipt's requests that started at or after since.
// The receipt covers every turn of the session, and so does the result's
// usage: a run that continues a session (a decision) would report what
// the earlier runs of that session already reported. On 5 November 2026
// the overview's cost of the day was three times what OpenRouter billed,
// because each decision counted the whole session again. It reports
// false when the receipt cannot tell, and the result's usage stands.
func usageSince(raw json.RawMessage, since time.Time) (domain.Usage, bool) {
	var receipt receiptObject
	if since.IsZero() || json.Unmarshal(raw, &receipt) != nil || receipt.Requests == nil {
		return domain.Usage{}, false
	}
	var usage domain.Usage
	for _, request := range *receipt.Requests {
		if request.StartedAt.Before(since) {
			continue
		}
		usage.Requests++
		usage.PromptTokens += request.PromptTokens
		usage.CachedTokens += request.CachedTokens
		usage.CompletionTokens += request.CompletionTokens
		usage.ReasoningTokens += request.ReasoningTokens
		if request.CostUSD != nil && *request.CostUSD > 0 {
			usage.CostUSD += *request.CostUSD
		}
	}
	return usage, true
}
