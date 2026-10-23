package application

import (
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// ExecutionRequest is everything the runtime needs for one execution:
// identity, the objective as the prompt, the model policy, the tool
// allow-list and the budget. AXLR does not enforce AllowedTools yet; the
// adapter passes them on once the runtime contract takes them.
type ExecutionRequest struct {
	ExecutionID   domain.ExecutionID
	TaskID        domain.TaskID
	BotID         domain.BotID
	TenantID      domain.TenantID
	CorrelationID domain.CorrelationID
	Objective     domain.Objective
	Model         string
	AllowedTools  []string
	Budgets       domain.Budgets
	Timeout       time.Duration
}
