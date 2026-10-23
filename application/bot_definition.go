package application

// BotDefinition is a bot as an operator writes it (examples/*.json). It
// is plain input; RegisterBot validates it into a domain.Bot.
type BotDefinition struct {
	ID           string                `json:"id,omitempty"`
	Tenant       string                `json:"tenant"`
	Name         string                `json:"name"`
	Owner        string                `json:"owner"`
	Mission      string                `json:"mission"`
	AllowedTools []string              `json:"allowedTools"`
	Model        ModelPolicyDefinition `json:"model"`
	Budgets      BudgetsDefinition     `json:"budgets"`
}
