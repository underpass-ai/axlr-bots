package nodeproto

import "github.com/underpass-ai/axlr-bots/domain"

// RunMessage is what the node needs to run a task's objective.
type RunMessage struct {
	TaskID         string                   `json:"taskId"`
	BotID          string                   `json:"botId"`
	TenantID       string                   `json:"tenantId"`
	CorrelationID  string                   `json:"correlationId,omitempty"`
	Objective      string                   `json:"objective"`
	Model          string                   `json:"model,omitempty"`
	Level          string                   `json:"level"`
	AllowedTools   []string                 `json:"allowedTools"`
	ToolRules      domain.ToolRulesSnapshot `json:"toolRules"`
	TimeoutSeconds int                      `json:"timeoutSeconds"`
	Budgets        *BudgetsMessage          `json:"budgets,omitempty"`
	Delegation     *DelegationOfferMessage  `json:"delegation,omitempty"`
}
