package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func toolRuleSnapshots(rules []*operatorv1.ToolRule) []domain.ToolRuleSnapshot {
	if len(rules) == 0 {
		return nil
	}
	out := make([]domain.ToolRuleSnapshot, len(rules))
	for i, rule := range rules {
		out[i] = domain.ToolRuleSnapshot{Tool: rule.GetTool(), Arguments: rule.GetArguments()}
		if len(out[i].Arguments) == 0 {
			out[i].Arguments = nil
		}
	}
	return out
}

func toolRuleMessages(rules []domain.ToolRuleSnapshot) []*operatorv1.ToolRule {
	out := make([]*operatorv1.ToolRule, len(rules))
	for i, rule := range rules {
		out[i] = &operatorv1.ToolRule{Tool: rule.Tool, Arguments: rule.Arguments}
	}
	return out
}

func toolRulesSnapshot(rules *operatorv1.ToolRules) domain.ToolRulesSnapshot {
	return domain.ToolRulesSnapshot{Allow: toolRuleSnapshots(rules.GetAllow()), Deny: toolRuleSnapshots(rules.GetDeny())}
}

func toolRulesMessage(rules domain.ToolRulesSnapshot) *operatorv1.ToolRules {
	return &operatorv1.ToolRules{Allow: toolRuleMessages(rules.Allow), Deny: toolRuleMessages(rules.Deny)}
}
