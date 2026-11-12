package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ReconcileGrace is what the default grace period adds to a bot's task
// timeout: an execution older than that has outlived any process that
// could still report it.
const ReconcileGrace = time.Minute

// ReconcileTasks ends the tasks a dead process left Running (vision
// §14.6). RunTask persists a task as Running before it calls the runtime,
// so a crash leaves a Running task on disk and nothing else ends it. This
// use case finds every Running task whose execution started more than a
// grace period ago and fails it with a diagnosis: the loss is explicit
// and no task stays Running forever. A younger task may still be running
// in another process and is left alone. A delegated task still Pending
// or waiting for a person past its deadline is failed too, and so is a
// Running execution its task does not run (an orphan).
type ReconcileTasks struct {
	deps Dependencies
	rec  recorder
	// InFlight, when set, reports a task this process is running right
	// now (SerialRunner's): such a task is never lost, whatever its age
	// and the grace, and is left alone. Nil in another process.
	InFlight func(domain.TaskID) bool
}

// NewReconcileTasks wires the use case.
func NewReconcileTasks(deps Dependencies) ReconcileTasks {
	return ReconcileTasks{deps: deps, rec: recorder{deps: deps}}
}

// Execute fails every Running task older than the grace period and
// reports which tasks it failed and which it skipped. A grace of zero or
// less means each bot's own: its task timeout (DefaultTaskTimeout when it
// sets none) plus ReconcileGrace. A task already failed stays failed if a
// later task's reconciliation fails with an error; running again is safe.
func (uc ReconcileTasks) Execute(ctx context.Context, grace time.Duration) (ReconcileTasksResult, error) {
	// The delegated tasks past their deadline go first: a Running task
	// failed below is then the pass's last change, and the caller's log
	// of it follows at once.
	missed, err := uc.missDeadlines(ctx)
	if err != nil {
		return ReconcileTasksResult{}, err
	}
	tasks, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskRunning)
	if err != nil {
		return ReconcileTasksResult{}, err
	}
	result := ReconcileTasksResult{Failed: missed, Skipped: []domain.TaskID{}, Orphaned: []domain.ExecutionID{}}
	if result.Failed == nil {
		result.Failed = []domain.TaskID{}
	}
	for _, task := range tasks {
		failed, err := uc.reconcile(ctx, task, grace)
		if err != nil {
			return ReconcileTasksResult{}, fmt.Errorf("reconcile task %s: %w", task.ID(), err)
		}
		if failed {
			result.Failed = append(result.Failed, task.ID())
		} else {
			result.Skipped = append(result.Skipped, task.ID())
		}
	}
	orphaned, err := uc.failOrphans(ctx, grace)
	if err != nil {
		return ReconcileTasksResult{}, err
	}
	result.Orphaned = append(result.Orphaned, orphaned...)
	return result, nil
}

// failOrphans fails the Running executions no Running task runs: a crash
// between saving an execution and saving its task (RunTask,
// NextAssignment, ApproveTask) leaves one behind, the pass over Running
// tasks never reaches it, and a node it is queued for would try to take
// it on every poll. The task is left alone; it is not running it. A
// younger orphan may be a start still between its two saves.
func (uc ReconcileTasks) failOrphans(ctx context.Context, grace time.Duration) ([]domain.ExecutionID, error) {
	running, err := uc.deps.Executions.ListRunning(ctx)
	if err != nil {
		return nil, err
	}
	var orphaned []domain.ExecutionID
	now := uc.deps.Clock.Now()
	for _, execution := range running {
		task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
		switch {
		case err != nil && !domain.IsNotFound(err):
			return nil, fmt.Errorf("reconcile execution %s: %w", execution.ID(), err)
		case err == nil && task.Status() == domain.TaskRunning && task.CurrentExecutionID() == execution.ID():
			continue
		}
		gone := err != nil
		after := grace
		if after <= 0 {
			after = DefaultTaskTimeout + ReconcileGrace
		}
		if !gone {
			// A bot deleted meanwhile leaves the default grace.
			bot, err := uc.deps.Bots.Get(ctx, task.BotID())
			switch {
			case err == nil:
				after = graceFor(bot, task, execution, grace)
			case !domain.IsNotFound(err):
				return nil, fmt.Errorf("reconcile execution %s: %w", execution.ID(), err)
			}
		}
		age := now.Sub(execution.StartedAt())
		if age <= after {
			continue
		}
		state := "is gone"
		if !gone {
			state = "is " + task.Status().String() + " and does not run it"
		}
		reason := "reconciled: orphan: the control plane found it Running after " + age.Round(time.Second).String() + " but its task " + execution.TaskID().String() + " " + state
		tracePath, _ := uc.deps.Traces.TracePath(execution.ID())
		if err := execution.Fail(reason, domain.Result{}, domain.Usage{}, tracePath, now); err != nil {
			return nil, err
		}
		if err := uc.deps.Executions.Save(ctx, execution); err != nil {
			if settledMeanwhile(err) {
				continue
			}
			return nil, fmt.Errorf("reconcile execution %s: %w", execution.ID(), err)
		}
		details := map[string]string{"task": execution.TaskID().String(), "age": age.Round(time.Second).String()}
		if err := uc.rec.audit(ctx, "execution.orphaned", execution.ID().String(), domain.AuditOK, details); err != nil {
			return nil, err
		}
		orphaned = append(orphaned, execution.ID())
	}
	return orphaned, nil
}

// settledMeanwhile reports a save refused because the execution ended
// in another process after it was read: that end stands. The store
// answers a domain.ConflictError for a copy that would end it again.
func settledMeanwhile(err error) bool {
	var conflict domain.ConflictError
	return errors.As(err, &conflict)
}

// graceFor is how long the execution may run before reconcile calls it
// lost: the grace asked for or, when that is zero or less, the bot's task
// timeout plus ReconcileGrace. A delegated task is never given past its
// grant's deadline plus ReconcileGrace: its run was bounded by the
// deadline, whatever the bot's own timeout.
func graceFor(bot domain.Bot, task domain.Task, execution domain.Execution, grace time.Duration) time.Duration {
	if grace <= 0 {
		grace = bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout) + ReconcileGrace
	}
	if lineage, ok := task.Lineage(); ok {
		if bound := lineage.Grant.Deadline.Add(ReconcileGrace).Sub(execution.StartedAt()); bound < grace {
			grace = bound
		}
	}
	return grace
}

// missDeadlines fails the delegated tasks still Pending or waiting for a
// person when their deadline came: nothing else would end a child whose
// node is gone or whose approval outlives its grant.
func (uc ReconcileTasks) missDeadlines(ctx context.Context) ([]domain.TaskID, error) {
	var missed []domain.TaskID
	now := uc.deps.Clock.Now()
	for _, status := range []domain.TaskStatus{domain.TaskPending, domain.TaskWaitingApproval} {
		tasks, err := uc.deps.Tasks.ListByStatus(ctx, status)
		if err != nil {
			return nil, err
		}
		for i := range tasks {
			if !tasks[i].DeadlinePassed(now) {
				continue
			}
			bot, err := uc.deps.Bots.Get(ctx, tasks[i].BotID())
			if domain.IsNotFound(err) {
				// Its bot is gone (a lost file: a delete ends its tasks
				// first); one task must not stop the pass.
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := newDeadlineKeeper(uc.deps).miss(ctx, bot, &tasks[i], nil); err != nil {
				return nil, fmt.Errorf("fail task %s past its deadline: %w", tasks[i].ID(), err)
			}
			missed = append(missed, tasks[i].ID())
		}
	}
	return missed, nil
}

// reconcile fails one Running task when its execution is older than the
// grace period, and reports whether it did. The bot is read for its
// budgets and its tenant, never for its status: a paused bot's lost task
// is still lost.
func (uc ReconcileTasks) reconcile(ctx context.Context, task domain.Task, grace time.Duration) (bool, error) {
	if uc.InFlight != nil && uc.InFlight(task.ID()) {
		return false, nil
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if domain.IsNotFound(err) {
		// Its bot is gone (a lost file: a delete ends its tasks first);
		// one task must not stop the pass.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	execution, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
	if err != nil {
		return false, err
	}
	grace = graceFor(bot, task, execution, grace)
	now := uc.deps.Clock.Now()
	age := now.Sub(execution.StartedAt())
	expired := execution.LeaseExpired(now)
	stranded, waits, err := uc.stranded(ctx, execution)
	if err != nil {
		return false, err
	}
	if age <= grace && !expired {
		// A decision queued for a node that is away waits for it (a
		// reboot or a reinstall takes minutes, and the run resumes);
		// only the timeout fails it. The task says what it waits for.
		if waits != nil {
			return false, uc.tellWaiting(ctx, bot, task, execution, waits)
		}
		return false, nil
	}
	// A report or a cancel may have ended the task since it was listed;
	// read both again so that end stands.
	if stillLost, err := uc.unchanged(ctx, task, execution); err != nil || !stillLost {
		return false, err
	}
	reason := diagnosis(execution, age, expired)
	if stranded != "" {
		reason = stranded
	}
	if execution.Outcome() == domain.ExecutionRunning {
		tracePath, _ := uc.deps.Traces.TracePath(execution.ID())
		// A run that started and was lost spent what nobody reported (a
		// node's late report may still tell it); one still queued for
		// its node never ran.
		usage := domain.UnknownUsage(0)
		if stranded != "" {
			usage = domain.Usage{}
		}
		if err := execution.Fail(reason, domain.Result{}, usage, tracePath, now); err != nil {
			return false, err
		}
		if err := uc.deps.Executions.Save(ctx, execution); err != nil {
			// A report settled it between the read and this save.
			if settledMeanwhile(err) {
				return false, nil
			}
			return false, err
		}
	}
	if err := task.Fail(reason, now); err != nil {
		return false, err
	}
	if err := uc.deps.Tasks.Save(ctx, task); err != nil {
		return false, err
	}
	payload := map[string]any{"executionId": execution.ID().String(), "reason": "reconciled"}
	if err := uc.rec.executionEvent(ctx, endEvent(task), bot, task.ID(), execution.ID(), task.CorrelationID(), payload); err != nil {
		return false, err
	}
	if err := newIncidentAdvancer(uc.deps).taskEnded(ctx, bot, task, execution); err != nil {
		return false, err
	}
	if err := newAnswerDeliverer(uc.deps).taskEnded(ctx, bot, task, execution); err != nil {
		return false, err
	}
	if err := newCompletionKeeper(uc.deps).taskEnded(ctx, bot, task); err != nil {
		return false, err
	}
	if err := newPlatformPublisher(uc.deps).taskEnded(ctx, bot, task, execution); err != nil {
		return false, err
	}
	details := map[string]string{"execution": execution.ID().String(), "age": age.Round(time.Second).String()}
	if err := uc.rec.audit(ctx, "task.reconcile", task.ID().String(), domain.AuditOK, details); err != nil {
		return false, err
	}
	return true, nil
}

// stranded explains why an execution queued for a node (a decision
// ApproveTask or an approval's deadline queued, not yet leased) does not
// run: its node is gone, or not Online. The session the decision
// continues lives on that node, so no other node can take it. It answers
// the failure reason the timeout gives it, and the payload of the event
// that tells the task's timeline; both empty when the execution is not
// queued or its node is Online.
func (uc ReconcileTasks) stranded(ctx context.Context, execution domain.Execution) (string, map[string]any, error) {
	if execution.Outcome() != domain.ExecutionRunning || !execution.IsAssigned() || !execution.LeaseUntil().IsZero() {
		return "", nil, nil
	}
	what := "its execution " + execution.ID().String()
	if execution.Kind() == domain.ExecutionDecide {
		what = "its decision (" + execution.Decision() + ", execution " + execution.ID().String() + ")"
	}
	nodeID := execution.NodeID().String()
	waits := map[string]any{"executionId": execution.ID().String(), "nodeId": nodeID}
	node, err := uc.deps.Nodes.Get(ctx, execution.NodeID())
	switch {
	case domain.IsNotFound(err):
		waits["nodeStatus"], waits["waiting"] = "gone", "waiting for node "+nodeID+", which is gone"
		return "reconciled: " + what + " waited for node " + nodeID + ", which is gone", waits, nil
	case err != nil:
		return "", nil, err
	case node.IsOnline():
		return "", nil, nil
	}
	seen := node.LastSeenAt().UTC()
	waits["nodeStatus"], waits["lastSeenAt"] = node.Status().String(), seen.Format(time.RFC3339Nano)
	waits["waiting"] = "waiting for node " + nodeID + ", " + strings.ToLower(node.Status().String()) + " since " + seen.Format("15:04") + " UTC"
	return "reconciled: " + what + " waited for node " + nodeID + ", which is " + node.Status().String() + " since it was last seen at " + seen.Format(time.RFC3339) + "; the session it continues is on that node", waits, nil
}

// tellWaiting records, once per state of the node, a node.offline event
// in the task's timeline: the decision waits for node X, offline since
// HH:MM. Before, the task showed Running with no hint while nothing ran
// (QA L9, 5 November 2026). The same state on the next pass records
// nothing; the node back and away again (a new last seen) records anew.
func (uc ReconcileTasks) tellWaiting(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution, waits map[string]any) error {
	timeline, err := uc.deps.EventReader.ListTaskEvents(ctx, task.ID(), MaxEventLimit)
	if err != nil {
		return err
	}
	for i := len(timeline) - 1; i >= 0; i-- {
		told := timeline[i]
		if told.Type != domain.EventNodeOffline || told.ExecutionID != execution.ID() {
			continue
		}
		if fmt.Sprint(told.Payload["nodeStatus"]) == fmt.Sprint(waits["nodeStatus"]) && fmt.Sprint(told.Payload["lastSeenAt"]) == fmt.Sprint(waits["lastSeenAt"]) {
			return nil
		}
		break
	}
	id, err := domain.NewEventID(uc.deps.IDs.NewID("evt-"))
	if err != nil {
		return err
	}
	// The node's event, told in the bot's tenant about its task.
	event, err := domain.NewNodeEvent(id, domain.EventNodeOffline, execution.NodeID(), uc.deps.Clock.Now(), waits)
	if err != nil {
		return err
	}
	event.TenantID, event.BotID, event.TaskID, event.CorrelationID, event.ExecutionID = bot.TenantID(), bot.ID(), task.ID(), task.CorrelationID(), execution.ID()
	if err := event.Validate(); err != nil {
		return err
	}
	return uc.deps.Events.Publish(ctx, event)
}

// unchanged reads the task and its execution again and reports whether
// they are as the pass found them: the task Running on that execution,
// and the execution with the same outcome.
func (uc ReconcileTasks) unchanged(ctx context.Context, task domain.Task, execution domain.Execution) (bool, error) {
	fresh, err := uc.deps.Tasks.Get(ctx, task.ID())
	if err != nil {
		return false, err
	}
	if fresh.Status() != domain.TaskRunning || fresh.CurrentExecutionID() != execution.ID() {
		return false, nil
	}
	current, err := uc.deps.Executions.Get(ctx, execution.ID())
	if err != nil {
		return false, err
	}
	return current.Outcome() == execution.Outcome(), nil
}

// diagnosis is the failure reason a reconciled task and execution carry.
// An execution that had already ended when its task was still Running
// (the process died between the two saves) is named as such.
func diagnosis(execution domain.Execution, age time.Duration, leaseExpired bool) string {
	found := "the control plane found it Running after " + age.Round(time.Second).String()
	if execution.Outcome() != domain.ExecutionRunning {
		found += " although its execution " + execution.ID().String() + " had already ended " + execution.Outcome().String()
	}
	if leaseExpired {
		return "reconciled: " + found + "; node " + execution.NodeID().String() + " stopped renewing its lease at " + execution.LeaseUntil().UTC().Format(time.RFC3339)
	}
	return "reconciled: " + found + "; the process that ran it is gone"
}
