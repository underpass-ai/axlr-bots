package domain

import "time"

// BotSnapshot is a bot as plain data: what repositories store and the CLI
// prints. RestoreBot turns it back into a validated Bot.
type BotSnapshot struct {
	ID           string              `json:"id"`
	TenantID     string              `json:"tenantId"`
	Name         string              `json:"name"`
	Owner        string              `json:"owner"`
	Mission      string              `json:"mission"`
	AllowedTools []string            `json:"allowedTools"`
	Model        ModelPolicySnapshot `json:"model"`
	Budgets      BudgetsSnapshot     `json:"budgets"`
	Level        string              `json:"level"`
	ToolRules    ToolRulesSnapshot   `json:"toolRules"`
	Triggers     []TriggerSnapshot   `json:"triggers"`
	Placement    PlacementSnapshot   `json:"placement"`
	Approvals    ApprovalsSnapshot   `json:"approvals"`
	Delegation   *DelegationSnapshot `json:"delegation,omitempty"`
	Status       string              `json:"status"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

// Redacted is the snapshot with every trigger credential and every
// notification secret hidden: what the API answers and the command
// prints. The repository keeps the unredacted snapshot; RestoreBot
// refuses a redacted one as a secret.
func (s BotSnapshot) Redacted() BotSnapshot {
	triggers := make([]TriggerSnapshot, len(s.Triggers))
	for i, t := range s.Triggers {
		triggers[i] = t.Redacted()
	}
	s.Triggers = triggers
	s.Approvals = s.Approvals.Redacted()
	return s
}
