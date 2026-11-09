package application

import (
	"context"
	"errors"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NextAssignment is what a node asks for: the next execution it should
// run. Executions already assigned to the node and not yet leased (a
// decision queued by ApproveTask, or an assignment the node lost before
// taking it) go first; then the oldest urgent Pending task of a bot
// placed on the node, else the oldest Pending one, is started and
// assigned to it. The node must be Online.
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
		if err == nil && (task.Status() != domain.TaskRunning || task.CurrentExecutionID() != execution.ID()) {
			err = domain.StateError{Subject: "task " + task.ID().String(), Current: task.Status().String() + " on " + string(task.CurrentExecutionID()), Attempted: "run its execution " + execution.ID().String()}
		}
		if err != nil {
			if err := uc.orphan(ctx, &execution, err); err != nil {
				return Assignment{}, false, err
			}
			continue
		}
		bot, err := uc.deps.Bots.Get(ctx, task.BotID())
		if domain.IsNotFound(err) {
			// Its bot is gone: nothing can describe the run, so it is
			// an orphan, as one whose task is gone.
			if err := uc.orphan(ctx, &execution, err); err != nil {
				return Assignment{}, false, err
			}
			continue
		}
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
		if execution.Kind() == domain.ExecutionRun {
			// A run re-delivered after a crash before its lease is asked
			// again, as a first delivery is.
			run, err := newLadderKeeper(uc.deps).recheck(ctx, bot, &task)
			if err != nil {
				return Assignment{}, false, err
			}
			if !run {
				continue
			}
		}
		assignment, err := uc.describe(ctx, bot, task, execution)
		switch {
		case isRefusal(err):
			if err := uc.refuse(ctx, bot, &task, &execution, err); err != nil {
				return Assignment{}, false, err
			}
			continue
		case errors.Is(err, errTooLate):
			if err := newDeadlineKeeper(uc.deps).miss(ctx, bot, &task, &execution); err != nil {
				return Assignment{}, false, err
			}
			continue
		case err != nil:
			if err := uc.orphan(ctx, &execution, err); err != nil {
				return Assignment{}, false, err
			}
			continue
		}
		return uc.lease(ctx, bot, task, execution, assignment)
	}
	return Assignment{}, false, nil
}

// orphan fails an execution assigned to the node that cannot run: its
// task is gone, no longer runs it (a crash between saving the execution
// and saving its task, in ApproveTask or startPending, leaves one), or it
// decides a call that is not there. Left Running, it would fail every
// poll of the node and starve it; failing it lets the node take its next
// task. Any other error is the poll's and is returned.
func (uc NextAssignment) orphan(ctx context.Context, execution *domain.Execution, cause error) error {
	var state domain.StateError
	if !errors.As(cause, &state) && !domain.IsNotFound(cause) {
		return cause
	}
	now := uc.deps.Clock.Now()
	tracePath, _ := uc.deps.Traces.TracePath(execution.ID())
	if err := execution.Fail("orphan: "+cause.Error(), domain.Result{}, domain.Usage{}, tracePath, now); err != nil {
		return err
	}
	if err := uc.deps.Executions.Save(ctx, *execution); err != nil {
		return err
	}
	details := map[string]string{"task": execution.TaskID().String(), "node": execution.NodeID().String(), "reason": cause.Error()}
	return uc.rec.audit(ctx, "execution.orphaned", execution.ID().String(), domain.AuditOK, details)
}

// isRefusal reports a task describe found cannot be handed to a node:
// the task fails, the poll does not.
func isRefusal(err error) bool {
	var policy errPolicyRefused
	var prompt errPromptRefused
	return errors.As(err, &policy) || errors.As(err, &prompt)
}

// refuse fails the task that cannot be handed to the node (no policy to
// run it under, no prompt that fits), with the reason.
func (uc NextAssignment) refuse(ctx context.Context, bot domain.Bot, task *domain.Task, execution *domain.Execution, refused error) error {
	failed := ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: refused.Error()}
	return newSettler(uc.deps).settle(ctx, "task.assign", bot, task, execution, failed, nil, uc.deps.Clock.Now())
}

// startPending starts the first Pending task the node may run: urgent
// ones, then the oldest. It reads the list once and goes down it, so a
// run of tasks that cannot start costs one pass under the runner's lock
// and the poll answers within its wait.
func (uc NextAssignment) startPending(ctx context.Context, node domain.Node) (Assignment, bool, error) {
	pending, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskPending)
	if err != nil {
		return Assignment{}, false, err
	}
	bots := map[domain.BotID]domain.Bot{}
	var runnable []domain.Task
	for i := range pending {
		candidate, err := uc.deps.Bots.Get(ctx, pending[i].BotID())
		if domain.IsNotFound(err) {
			// A task of a bot that is gone is no task for this node,
			// and must not keep the others from it.
			continue
		}
		if err != nil {
			return Assignment{}, false, err
		}
		if !candidate.AcceptsTasks() || !nodeCanRun(candidate, node) {
			continue
		}
		if !pending[i].Skills().IsEmpty() && !node.Capabilities().SkillsSupported {
			// The task keeps the skills it was submitted with, whatever
			// its bot has now: a node that cannot read them does not take it.
			continue
		}
		if pending[i].DeadlinePassed(uc.deps.Clock.Now()) {
			// A delegated task that waited past its deadline never runs.
			if err := newDeadlineKeeper(uc.deps).miss(ctx, candidate, &pending[i], nil); err != nil {
				return Assignment{}, false, err
			}
			continue
		}
		bots[candidate.ID()] = candidate
		runnable = append(runnable, pending[i])
	}
	slices.SortStableFunc(runnable, func(a, b domain.Task) int {
		switch {
		case goesBefore(a, b):
			return -1
		case goesBefore(b, a):
			return 1
		}
		return 0
	})
	for i, task := range runnable {
		if i > 0 {
			// The tasks ended before this one may have moved it (a
			// cancelled child): the list is not read again, the task is.
			fresh, err := uc.deps.Tasks.Get(ctx, task.ID())
			if domain.IsNotFound(err) {
				continue
			}
			if err != nil {
				return Assignment{}, false, err
			}
			if fresh.Status() != domain.TaskPending {
				continue
			}
			task = fresh
		}
		assignment, found, err := uc.start(ctx, node, bots[task.BotID()], task)
		if err != nil || found {
			return assignment, found, err
		}
	}
	return Assignment{}, false, nil
}

// start starts one task and assigns it to the node. False with no error
// says it did not go out, for a reason of its own: the ladder held it,
// or it was refused or missed its deadline, and is over with the reason.
// The next task may go.
func (uc NextAssignment) start(ctx context.Context, node domain.Node, bot domain.Bot, task domain.Task) (Assignment, bool, error) {
	run, err := newLadderKeeper(uc.deps).recheck(ctx, bot, &task)
	if err != nil {
		return Assignment{}, false, err
	}
	if !run {
		return Assignment{}, false, nil
	}
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
	if err := keepNotes(ctx, uc.deps, bot, &task); err != nil {
		return Assignment{}, false, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return Assignment{}, false, err
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskStarted, bot, task.ID(), executionID, task.CorrelationID(), map[string]any{"executionId": executionID.String(), "nodeId": node.ID().String()}); err != nil {
		return Assignment{}, false, err
	}
	assignment, err := uc.describe(ctx, bot, task, execution)
	if isRefusal(err) {
		return Assignment{}, false, uc.refuse(ctx, bot, &task, &execution, err)
	}
	if errors.Is(err, errTooLate) {
		// The deadline came while the task was being started.
		return Assignment{}, false, newDeadlineKeeper(uc.deps).miss(ctx, bot, &task, &execution)
	}
	if err != nil {
		return Assignment{}, false, err
	}
	return uc.lease(ctx, bot, task, execution, assignment)
}

// goesBefore reports whether a task is assigned before another: an
// urgent one first, then the older.
func goesBefore(a, b domain.Task) bool {
	if a.IsUrgent() != b.IsUrgent() {
		return a.IsUrgent()
	}
	return a.CreatedAt().Before(b.CreatedAt())
}

// describe builds what the node needs for the execution: a delegated
// task runs with its grant and until its deadline, a bot that may
// delegate gets the delegation tool in its allow-list, and a bot in a
// team reads its teammates between its mission and the task.
func (uc NextAssignment) describe(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution) (Assignment, error) {
	assignment := Assignment{ExecutionID: execution.ID(), Kind: execution.Kind()}
	timeout, ok := runTimeout(bot, task, uc.deps.Clock.Now())
	if !ok {
		return Assignment{}, errTooLate
	}
	mates, err := teammatesOf(ctx, uc.deps, bot)
	if err != nil {
		return Assignment{}, err
	}
	offer, events := delegationOffer(bot, task, mates), eventsOffer(bot, mates)
	// The task's incident phase may narrow the bot's level and tools; the
	// decision runs under the same policy as the run it continues. A
	// narrowed phase gets exactly its tools, as in process: the node's
	// delegate, emit and subscribe tools only when it lists them.
	policy, _, policyErr := bot.PolicyOf(task)
	if policyErr != nil {
		return Assignment{}, errPolicyRefused{cause: policyErr}
	}
	if policy.Narrowed() {
		if !policy.Tools().Allows(DelegateToolName) {
			offer = nil
		}
		if !policy.Tools().Allows(EmitEventToolName) && !policy.Tools().Allows(SubscribeToolName) {
			events = nil
		}
	}
	tools := allowedTools(policy.Tools(), offer, events)
	if policy.Narrowed() {
		tools = policy.Tools().Names()
	}
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
			Level: policy.Level(), AllowedTools: tools, ToolRules: policy.Rules(), Timeout: timeout, Delegation: offer, Events: events,
			Skills: task.Skills(),
		}
		return assignment, nil
	}
	prompt, err := composedPrompt(bot, task, mates)
	if err != nil {
		return Assignment{}, err
	}
	assignment.Run = &ExecutionRequest{
		ExecutionID: execution.ID(), TaskID: task.ID(), BotID: bot.ID(), TenantID: bot.TenantID(), CorrelationID: task.CorrelationID(),
		Objective: task.Objective(), Prompt: prompt, Model: bot.ModelPolicy().Model(), Level: policy.Level(), AllowedTools: tools,
		ToolRules: policy.Rules(), Unattended: policy.Unattended(), Budgets: taskBudgets(bot, task), Timeout: timeout, Delegation: offer, Events: events,
		Runtime: bot.Runtime(), Input: task.Input(), Skills: task.Skills(),
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
