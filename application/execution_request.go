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
	// Delegation is set when the bot may ask other bots: the node then
	// serves the delegation tool to the execution.
	Delegation *DelegationOffer
	// Events is set when the bot is in a team: the node then serves the
	// emit tool to the execution.
	Events *EventsOffer
	// DelegationSocket is where the node serves those tools for this
	// execution; the node sets it, it never travels.
	DelegationSocket string
	// OnSaid hears what the run says back to an operator's message as
	// it says it; the node sets it, it never travels.
	OnSaid func(SaidReply) `json:"-"`
}

// NodeTools names the tools the node serves the execution on its
// socket: the delegation tool and the emit tool, as offered.
func (r ExecutionRequest) NodeTools() []string { return nodeTools(r.Delegation, r.Events) }

func nodeTools(delegation *DelegationOffer, events *EventsOffer) []string {
	var tools []string
	if delegation != nil {
		tools = append(tools, DelegateToolName)
	}
	if events != nil {
		tools = append(tools, EmitEventToolName, SubscribeToolName)
	}
	return tools
}
