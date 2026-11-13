package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// activityUsage maps usage to the wire, the cost unknown kept apart from
// a cost of zero.
func activityUsage(u domain.Usage) *operatorv1.Usage {
	return &operatorv1.Usage{
		Requests: int32(u.Requests), PromptTokens: int32(u.PromptTokens), CachedTokens: int32(u.CachedTokens),
		CompletionTokens: int32(u.CompletionTokens), ReasoningTokens: int32(u.ReasoningTokens),
		ToolCalls: int32(u.ToolCalls), CostUsd: u.CostUSD, CostUnknown: u.CostUnknown,
	}
}
