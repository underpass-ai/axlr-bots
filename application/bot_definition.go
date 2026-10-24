package application

import "github.com/underpass-ai/underpass-bots/domain"

// BotDefinition is a bot as an operator writes it (examples/*.json). It
// is plain input; RegisterBot validates it into a domain.Bot. Level is
// the autonomy level (observe when absent) and ToolRules narrows the
// allowed tools by their arguments, in the runtime contract's words.
type BotDefinition struct {
	ID           string                   `json:"id,omitempty"`
	Tenant       string                   `json:"tenant"`
	Name         string                   `json:"name"`
	Owner        string                   `json:"owner"`
	Mission      string                   `json:"mission"`
	AllowedTools []string                 `json:"allowedTools"`
	Level        string                   `json:"level,omitempty"`
	ToolRules    domain.ToolRulesSnapshot `json:"toolRules"`
	Model        ModelPolicyDefinition    `json:"model"`
	Budgets      BudgetsDefinition        `json:"budgets"`
}
