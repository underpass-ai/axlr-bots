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
	Status       string              `json:"status"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}
