package application

import "github.com/underpass-ai/axlr-bots/domain"

// BotDefinition is a bot as an operator writes it (examples/*.json). It
// is plain input; RegisterBot validates it into a domain.Bot. Level is
// the autonomy level (observe when absent), ToolRules narrows the
// allowed tools by their arguments in the runtime contract's words,
// Triggers says how tasks reach the bot (manual when absent), Placement
// which nodes run them (any when absent), Approvals how a pending
// call waits for a person (an hour, then deny, when absent),
// Delegation which bots outside its teams it may ask to do a task (none
// when absent) and Groups the teams it belongs to (none when absent).
type BotDefinition struct {
	ID           string                     `json:"id,omitempty"`
	Tenant       string                     `json:"tenant"`
	Name         string                     `json:"name"`
	Owner        string                     `json:"owner"`
	Mission      string                     `json:"mission"`
	AllowedTools []string                   `json:"allowedTools"`
	Level        string                     `json:"level,omitempty"`
	ToolRules    domain.ToolRulesSnapshot   `json:"toolRules"`
	Triggers     []domain.TriggerSnapshot   `json:"triggers"`
	Placement    domain.PlacementSnapshot   `json:"placement"`
	Approvals    domain.ApprovalsSnapshot   `json:"approvals"`
	Delegation   *domain.DelegationSnapshot `json:"delegation,omitempty"`
	Groups       []string                   `json:"groups,omitempty"`
	Events       *domain.EventsSnapshot     `json:"events,omitempty"`
	Model        ModelPolicyDefinition      `json:"model"`
	Budgets      BudgetsDefinition          `json:"budgets"`
}
