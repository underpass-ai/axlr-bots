package application

import "github.com/underpass-ai/axlr-bots/domain"

// BotPatch is a partial change to a bot's definition (PATCH
// /v1/bots/{id}): every field is optional and replaces the bot's whole
// value for it; a nil field leaves the bot's. The identity (id, tenant,
// name, owner) does not change.
type BotPatch struct {
	Mission      *string                    `json:"mission,omitempty"`
	Level        *string                    `json:"level,omitempty"`
	AllowedTools *[]string                  `json:"allowedTools,omitempty"`
	ToolRules    *domain.ToolRulesSnapshot  `json:"toolRules,omitempty"`
	Triggers     *[]domain.TriggerSnapshot  `json:"triggers,omitempty"`
	Placement    *domain.PlacementSnapshot  `json:"placement,omitempty"`
	Approvals    *domain.ApprovalsSnapshot  `json:"approvals,omitempty"`
	Delegation   *domain.DelegationSnapshot `json:"delegation,omitempty"`
	Model        *ModelPolicyDefinition     `json:"model,omitempty"`
	Budgets      *BudgetsDefinition         `json:"budgets,omitempty"`
}
