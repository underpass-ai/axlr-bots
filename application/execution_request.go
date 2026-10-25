package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionRequest is everything the runtime needs for one execution:
// identity, the prompt (the bot's mission and the task's objective), the
// model policy, the policy
// (the autonomy level, the tool allow-list and the rules that narrow it)
// and the budget. The adapter hands the policy to the runtime when the
// runtime takes one and records whether it did.
type ExecutionRequest struct {
	ExecutionID   domain.ExecutionID
	TaskID        domain.TaskID
	BotID         domain.BotID
	TenantID      domain.TenantID
	CorrelationID domain.CorrelationID
	Objective     domain.Objective
	// Prompt is what the runtime receives: the mission and the objective.
	Prompt       domain.Prompt
	Model        string
	Level        domain.AutonomyLevel
	AllowedTools []string
	ToolRules    domain.ToolRules
	Budgets      domain.Budgets
	Timeout      time.Duration
}
