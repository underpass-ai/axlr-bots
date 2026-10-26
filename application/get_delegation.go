package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetDelegation is a node reading a child its execution's task
// delegated: its state, its answer once it Completed and how long the
// parent may still wait. A node sees only the children of the task
// whose execution it holds.
type GetDelegation struct {
	deps Dependencies
}

// NewGetDelegation wires the use case.
func NewGetDelegation(deps Dependencies) GetDelegation { return GetDelegation{deps: deps} }

// Execute answers the child's state.
func (uc GetDelegation) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, childID domain.TaskID) (DelegationAnswer, error) {
	execution, err := runningExecutionOf(ctx, uc.deps, node, executionID, "read a delegation")
	if err != nil {
		return DelegationAnswer{}, err
	}
	parent, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return DelegationAnswer{}, err
	}
	child, err := uc.deps.Tasks.Get(ctx, childID)
	if err != nil {
		return DelegationAnswer{}, err
	}
	lineage, ok := child.Lineage()
	if !ok || lineage.ParentTaskID != parent.ID().String() {
		// Another task's child is not this node's to read.
		return DelegationAnswer{}, domain.NotFoundError{Kind: "delegation", ID: childID.String()}
	}
	bot, err := uc.deps.Bots.Get(ctx, parent.BotID())
	if err != nil {
		return DelegationAnswer{}, err
	}
	answer := answerFor(child, executionDeadline(bot, parent, execution).Add(-DelegationWaitMargin))
	answer.Depth, answer.Grant = lineage.Depth, lineage.Grant
	if current := child.CurrentExecutionID(); current != "" {
		ran, err := uc.deps.Executions.Get(ctx, current)
		if err != nil {
			return DelegationAnswer{}, err
		}
		if child.Status() == domain.TaskCompleted {
			answer.Answer = ran.Result().Answer
		}
		if answer.FailureReason == "" && (child.Status() == domain.TaskFailed || child.Status() == domain.TaskExpired) {
			answer.FailureReason = ran.FailureReason()
		}
	}
	return answer, nil
}
