package application

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NextAssignment is what a node asks for: the next execution it should
// run. Executions already assigned to the node and not yet leased (a
// decision queued by ApproveTask, or an assignment the node lost before
// taking it) go first; then the oldest Pending task of a bot placed on
// the node is started and assigned to it. The node must be Online.
type NextAssignment struct {
	deps Dependencies
	rec  recorder
}

// NewNextAssignment wires the use case.
func NewNextAssignment(deps Dependencies) NextAssignment {
	return NextAssignment{deps: deps, rec: recorder{deps: deps}}
}

// Execute returns the assignment and true, or false when the node has
// nothing to do.
func (uc NextAssignment) Execute(ctx context.Context, nodeID domain.NodeID) (Assignment, bool, error) {
	node, err := uc.deps.Nodes.Get(ctx, nodeID)
	if err != nil {
		return Assignment{}, false, err
	}
	if !node.IsOnline() {
		return Assignment{}, false, domain.StateError{Subject: "node " + nodeID.String(), Current: node.Status().String(), Attempted: "take an assignment"}
	}
	if assignment, found, err := uc.queued(ctx, node); err != nil || found {
		return assignment, found, err
	}
	return uc.startPending(ctx, node)
}

// queued leases an execution already assigned to the node.
func (uc NextAssignment) queued(ctx context.Context, node domain.Node) (Assignment, bool, error) {
	running, err := uc.deps.Executions.ListRunning(ctx)
	if err != nil {
		return Assignment{}, false, err
	}
	for _, execution := range running {
		if execution.NodeID() != node.ID() || !execution.LeaseUntil().IsZero() {
			continue
		}
		task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
		if err != nil {
			return Assignment{}, false, err
		}
		bot, err := uc.deps.Bots.Get(ctx, task.BotID())
		if err != nil {
			return Assignment{}, false, err
		}
		if task.DeadlinePassed(uc.deps.Clock.Now()) {
			// A decision that would start after the delegation's
			// deadline runs nothing.
			if err := newDeadlineKeeper(uc.deps).miss(ctx, bot, &task, &execution); err != nil {
				return Assignment{}, false, err
			}
			continue
		}
		assignment, err := uc.describe(ctx, bot, task, execution)
		if err != nil {
			return Assignment{}, false, err
		}
		return uc.lease(ctx, bot, task, execution, assignment)
	}
	return Assignment{}, false, nil
}

// startPending starts the oldest Pending task the node may run.
func (uc NextAssignment) startPending(ctx context.Context, node domain.Node) (Assignment, bool, error) {
	pending, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskPending)
	if err != nil {
		return Assignment{}, false, err
	}
	var chosen *domain.Task
	var bot domain.Bot
	for i := range pending {
		candidate, err := uc.deps.Bots.Get(ctx, pending[i].BotID())
		if err != nil {
			return Assignment{}, false, err
		}
		if !candidate.AcceptsTasks() || !candidate.Placement().Admits(node) {
			continue
		}
		if pending[i].DeadlinePassed(uc.deps.Clock.Now()) {
			// A delegated task that waited past its deadline never runs.
			if err := newDeadlineKeeper(uc.deps).miss(ctx, candidate, &pending[i], nil); err != nil {
				return Assignment{}, false, err
			}
			continue
		}
		if chosen == nil || pending[i].CreatedAt().Before(chosen.CreatedAt()) {
			chosen, bot = &pending[i], candidate
		}
	}
	if chosen == nil {
		return Assignment{}, false, nil
	}
	task := *chosen
	executionID, err := domain.NewExecutionID(uc.deps.IDs.NewID("exec-"))
	if err != nil {
		return Assignment{}, false, err
	}
	now := uc.deps.Clock.Now()
	if err := task.Start(executionID, now); err != nil {
		return Assignment{}, false, err
	}
	execution, err := domain.NewExecution(executionID, task.ID(), bot.ID(), now)
	if err != nil {
		return Assignment{}, false, err
	}
	if err := execution.Correlate(task.CorrelationID()); err != nil {
		return Assignment{}, false, err
	}
	if err := execution.AssignTo(node.ID()); err != nil {
		return Assignment{}, false, err
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		return Assignment{}, false, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return Assignment{}, false, err
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskStarted, bot, task.ID(), executionID, task.CorrelationID(), map[string]any{"executionId": executionID.String(), "nodeId": node.ID().String()}); err != nil {
		return Assignment{}, false, err
	}
	assignment, err := uc.describe(ctx, bot, task, execution)
	if err != nil {
		return Assignment{}, false, err
	}
	return uc.lease(ctx, bot, task, execution, assignment)
}

// describe builds what the node needs for the execution: a delegated
// task runs with its grant and until its deadline, and a bot that may
// delegate gets the delegation tool in its allow-list.
func (uc NextAssignment) describe(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution) (Assignment, error) {
	assignment := Assignment{ExecutionID: execution.ID(), Kind: execution.Kind()}
	timeout := runTimeout(bot, task, uc.deps.Clock.Now())
	if execution.Kind() == domain.ExecutionDecide {
		previous, decision, err := decisionOf(ctx, uc.deps, task, execution)
		if err != nil {
			return Assignment{}, err
		}
		pending := previous.Result().PendingDecision
		sessionID := pending.SessionID
		if sessionID == "" {
			sessionID = previous.Result().SessionID
		}
		assignment.Decide = &DecisionRequest{
			ExecutionID: execution.ID(), TaskID: task.ID(), BotID: bot.ID(), CorrelationID: task.CorrelationID(), SessionID: sessionID, CallID: pending.CallID, Decision: decision,
			Level: bot.Level(), AllowedTools: allowedTools(bot), ToolRules: bot.ToolRules(), Timeout: timeout, Delegation: delegationOffer(bot, task),
		}
		return assignment, nil
	}
	prompt, err := domain.NewPrompt(bot.Mission(), task.Objective())
	if err != nil {
		return Assignment{}, err
	}
	assignment.Run = &ExecutionRequest{
		ExecutionID: execution.ID(), TaskID: task.ID(), BotID: bot.ID(), TenantID: bot.TenantID(), CorrelationID: task.CorrelationID(),
		Objective: task.Objective(), Prompt: prompt, Model: bot.ModelPolicy().Model(), Level: bot.Level(), AllowedTools: allowedTools(bot),
		ToolRules: bot.ToolRules(), Budgets: taskBudgets(bot, task), Timeout: timeout, Delegation: delegationOffer(bot, task),
	}
	return assignment, nil
}

// lease gives the node the execution and tells it.
func (uc NextAssignment) lease(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution, assignment Assignment) (Assignment, bool, error) {
	until := uc.deps.Clock.Now().Add(LeaseDuration)
	if err := execution.Lease(until); err != nil {
		return Assignment{}, false, err
	}
	if err := uc.deps.Executions.Save(ctx, execution); err != nil {
		// The execution ended between its creation and this lease: an
		// operator cancelled the task in that moment. There is nothing for
		// the node to run, which is not the node's error.
		var state domain.StateError
		if errors.As(err, &state) {
			return Assignment{}, false, nil
		}
		return Assignment{}, false, err
	}
	assignment.LeaseUntil = until
	payload := map[string]any{"executionId": execution.ID().String(), "nodeId": execution.NodeID().String(), "kind": execution.Kind().String(), "leaseUntil": until}
	if err := uc.rec.executionEvent(ctx, domain.EventExecutionAssigned, bot, task.ID(), execution.ID(), task.CorrelationID(), payload); err != nil {
		return Assignment{}, false, err
	}
	return assignment, true, nil
}

// decisionOf finds the execution a decision continues and the decision
// itself, recorded by ApproveTask in the task's decided event: the
// previous execution of the task holds the pending call.
func decisionOf(ctx context.Context, deps Dependencies, task domain.Task, execution domain.Execution) (domain.Execution, string, error) {
	ids := task.ExecutionIDs()
	var previousID domain.ExecutionID
	for i, id := range ids {
		if id == execution.ID() && i > 0 {
			previousID = ids[i-1]
		}
	}
	if previousID == "" {
		return domain.Execution{}, "", domain.StateError{Subject: "execution " + execution.ID().String(), Current: "decide", Attempted: "find the call it decides"}
	}
	previous, err := deps.Executions.Get(ctx, previousID)
	if err != nil {
		return domain.Execution{}, "", err
	}
	if previous.Result().PendingDecision == nil {
		return domain.Execution{}, "", domain.StateError{Subject: "execution " + previousID.String(), Current: previous.Outcome().String(), Attempted: "decide a pending call"}
	}
	return previous, execution.Decision(), nil
}
