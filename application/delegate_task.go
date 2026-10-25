package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DelegateTask is a running execution asking, through its node, that
// another bot do a task. The control plane derives everything from the
// execution the node holds, never from the request: the bot that asks,
// its task, its depth, its chain and what it has left. It checks, in
// order, that the bot may ask the target, that the target takes
// delegated tasks, the chain's depth, that the target is not already on
// the chain, and the budget it can grant; the first that fails is the
// refusal. Otherwise it creates the child under the parent's trace id,
// with its lineage and its grant, and records it on the parent.
type DelegateTask struct {
	deps Dependencies
	rec  recorder
}

// NewDelegateTask wires the use case.
func NewDelegateTask(deps Dependencies) DelegateTask {
	return DelegateTask{deps: deps, rec: recorder{deps: deps}}
}

// delegation is one ask being checked: the parent's side.
type delegation struct {
	node      domain.NodeID
	execution domain.Execution
	task      domain.Task
	bot       domain.Bot
	target    domain.BotID
	objective domain.Objective
	wait      bool
}

// Execute creates the child, finds the one the same ask created, or
// answers why not.
func (uc DelegateTask) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command DelegateCommand) (DelegationAnswer, error) {
	d, err := uc.load(ctx, node, executionID, command)
	if err != nil {
		return DelegationAnswer{}, err
	}
	now := uc.deps.Clock.Now()
	waitUntil := executionDeadline(d.bot, d.task, d.execution).Add(-DelegationWaitMargin)
	if !d.bot.Delegation().Allows(d.target) {
		return uc.refuse(ctx, d, domain.RefusedNotAllowed, fmt.Sprintf("bot %s may not delegate to %s; it may ask: %s", d.bot.ID(), d.target, joinBots(d.bot.Delegation().To())))
	}
	target, reason, err := uc.targetOf(ctx, d)
	if err != nil {
		return DelegationAnswer{}, err
	}
	if reason != "" {
		return uc.refuse(ctx, d, domain.RefusedTargetUnavailable, reason)
	}
	limit := chainDepthLimit(d.bot, d.task)
	depth := d.task.Depth() + 1
	if depth > limit {
		return uc.refuse(ctx, d, domain.RefusedDepthExceeded, fmt.Sprintf("a task of %s would be at depth %d; this chain allows %d", d.target, depth, limit))
	}
	lineage, _ := d.task.Lineage()
	if d.target == d.bot.ID() || lineage.Visited(d.target) {
		return uc.refuse(ctx, d, domain.RefusedCycle, fmt.Sprintf("bot %s is already on this chain (%s): asking it again would loop", d.target, strings.Join(append(lineage.Chain, d.bot.ID().String()), " → ")))
	}
	key, err := delegationKey(d.task.ID(), d.target, d.objective)
	if err != nil {
		return DelegationAnswer{}, err
	}
	if existing, found, err := uc.deps.Tasks.FindByIdempotencyKey(ctx, d.target, key); err != nil {
		return DelegationAnswer{}, err
	} else if found {
		return uc.again(ctx, d, existing, waitUntil, now)
	}
	grant, reason := uc.grant(d, target, now)
	if reason != "" {
		return uc.refuse(ctx, d, domain.RefusedBudgetExhausted, reason)
	}
	child := domain.TaskLineage{
		ParentTaskID: d.task.ID().String(), ParentExecutionID: d.execution.ID().String(), ParentBotID: d.bot.ID().String(),
		Depth: depth, MaxDepth: limit, Chain: append(lineage.Chain, d.bot.ID().String()), Grant: grant,
	}
	input, _ := json.Marshal(map[string]any{"parentTaskId": child.ParentTaskID, "parentBotId": child.ParentBotID, "depth": depth})
	created, err := submitTask(ctx, uc.deps, submission{
		action: "task.delegated", bot: target, trigger: domain.DelegationTrigger(), objective: d.objective,
		key: key, correlation: d.task.CorrelationID(), input: input, lineage: &child,
	})
	if err != nil {
		return DelegationAnswer{}, err
	}
	if err := d.task.AdoptChild(created.Task.ID(), grant, now); err != nil {
		return DelegationAnswer{}, err
	}
	if err := uc.deps.Tasks.Save(ctx, d.task); err != nil {
		return DelegationAnswer{}, err
	}
	if err := uc.recordDelegated(ctx, d, created.Task, child, false); err != nil {
		return DelegationAnswer{}, err
	}
	return DelegationAnswer{TaskID: created.Task.ID(), BotID: d.target, Status: created.Task.Status(), Created: true, Depth: depth, Grant: grant, WaitUntil: waitUntil}, nil
}

// load finds the execution the node holds and the task and bot behind it.
func (uc DelegateTask) load(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command DelegateCommand) (delegation, error) {
	execution, err := runningExecutionOf(ctx, uc.deps, node, executionID, "delegate")
	if err != nil {
		return delegation{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return delegation{}, err
	}
	if task.Status() != domain.TaskRunning {
		return delegation{}, domain.StateError{Subject: "task " + task.ID().String(), Current: task.Status().String(), Attempted: "delegate"}
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return delegation{}, err
	}
	target, err := domain.NewBotID(command.Bot)
	if err != nil {
		return delegation{}, err
	}
	objective, err := domain.NewObjective(command.Objective)
	if err != nil {
		return delegation{}, err
	}
	return delegation{node: node, execution: execution, task: task, bot: bot, target: target, objective: objective, wait: command.Wait}, nil
}

// targetOf loads the target and says why it cannot take the task, if
// it cannot.
func (uc DelegateTask) targetOf(ctx context.Context, d delegation) (domain.Bot, string, error) {
	target, err := uc.deps.Bots.Get(ctx, d.target)
	switch {
	case domain.IsNotFound(err):
		return domain.Bot{}, "bot " + d.target.String() + " does not exist", nil
	case err != nil:
		return domain.Bot{}, "", err
	case target.TenantID() != d.bot.TenantID():
		return domain.Bot{}, "bot " + d.target.String() + " belongs to another tenant", nil
	case !target.AcceptsTasks():
		return domain.Bot{}, "bot " + d.target.String() + " is " + target.Status().String() + " and takes no task", nil
	case !target.AcceptsTrigger(domain.TriggerDelegation):
		return domain.Bot{}, "bot " + d.target.String() + " takes no delegated task: its triggers do not include delegation", nil
	}
	return target, "", nil
}

// grant shares what the parent has left with the child, or says why
// there is not enough.
func (uc DelegateTask) grant(d delegation, target domain.Bot, now time.Time) (domain.DelegationGrant, string) {
	share := d.bot.Delegation().BudgetShare()
	pool := taskBudgets(d.bot, d.task)
	grantedModel, grantedTools := d.task.Granted()
	own := target.Budgets()
	model, ok := shareOf(pool.MaxModelCallsPerTask(), grantedModel, share, own.MaxModelCallsPerTask())
	if !ok {
		return domain.DelegationGrant{}, fmt.Sprintf("this task has %d of its %d model calls left to share, and %.2f of them is under one", pool.MaxModelCallsPerTask()-grantedModel, pool.MaxModelCallsPerTask(), share)
	}
	tools, ok := shareOf(pool.MaxToolCallsPerTask(), grantedTools, share, own.MaxToolCallsPerTask())
	if !ok {
		return domain.DelegationGrant{}, fmt.Sprintf("this task has %d of its %d tool calls left to share, and %.2f of them is under one", pool.MaxToolCallsPerTask()-grantedTools, pool.MaxToolCallsPerTask(), share)
	}
	left := executionDeadline(d.bot, d.task, d.execution).Sub(now)
	span := time.Duration(share * float64(left))
	if ownTimeout := own.TaskTimeoutOr(DefaultTaskTimeout); span > ownTimeout {
		span = ownTimeout
	}
	if span < MinDelegationTime {
		return domain.DelegationGrant{}, fmt.Sprintf("this task has %s left; %.2f of it is under the %s a delegated task needs", left.Truncate(time.Second), share, MinDelegationTime)
	}
	return domain.DelegationGrant{MaxModelCalls: model, MaxToolCalls: tools, Deadline: now.Add(span)}, ""
}

// again answers an ask the same task made before with the child it
// created, adopting it if a crash left the parent without it.
func (uc DelegateTask) again(ctx context.Context, d delegation, child domain.Task, waitUntil, now time.Time) (DelegationAnswer, error) {
	lineage, ok := child.Lineage()
	if !ok || lineage.ParentTaskID != d.task.ID().String() {
		return DelegationAnswer{}, domain.StateError{Subject: "task " + child.ID().String(), Current: "created by another ask", Attempted: "answer this delegation"}
	}
	adopted := false
	for _, id := range d.task.Children() {
		adopted = adopted || id == child.ID()
	}
	if !adopted {
		if err := d.task.AdoptChild(child.ID(), lineage.Grant, now); err != nil {
			return DelegationAnswer{}, err
		}
		if err := uc.deps.Tasks.Save(ctx, d.task); err != nil {
			return DelegationAnswer{}, err
		}
	}
	if err := uc.recordDelegated(ctx, d, child, lineage, true); err != nil {
		return DelegationAnswer{}, err
	}
	answer := answerFor(child, waitUntil)
	answer.Depth, answer.Grant = lineage.Depth, lineage.Grant
	return answer, nil
}

func (uc DelegateTask) recordDelegated(ctx context.Context, d delegation, child domain.Task, lineage domain.TaskLineage, deduplicated bool) error {
	payload := map[string]any{
		"childTaskId": child.ID().String(), "childBotId": child.BotID().String(), "depth": lineage.Depth, "maxDepth": lineage.MaxDepth,
		"chain": lineage.Chain, "wait": d.wait, "deduplicated": deduplicated,
		"grant": map[string]any{"maxModelCalls": lineage.Grant.MaxModelCalls, "maxToolCalls": lineage.Grant.MaxToolCalls, "deadline": lineage.Grant.Deadline},
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskDelegated, d.bot, d.task.ID(), d.execution.ID(), d.task.CorrelationID(), payload); err != nil {
		return err
	}
	details := map[string]string{"node": d.node.String(), "execution": d.execution.ID().String(), "target": d.target.String(), "child": child.ID().String(), "depth": strconv.Itoa(lineage.Depth), "deduplicated": strconv.FormatBool(deduplicated)}
	return uc.rec.audit(ctx, "task.delegate", d.task.ID().String(), domain.AuditOK, details)
}

// refuse records the refusal and answers it.
func (uc DelegateTask) refuse(ctx context.Context, d delegation, reason, message string) (DelegationAnswer, error) {
	refusal := domain.DelegationRefusal{Reason: reason, Message: message}
	payload := map[string]any{"target": d.target.String(), "reason": reason, "message": message, "depth": d.task.Depth() + 1, "objective": d.objective.String()}
	if err := uc.rec.executionEvent(ctx, domain.EventDelegationRefused, d.bot, d.task.ID(), d.execution.ID(), d.task.CorrelationID(), payload); err != nil {
		return DelegationAnswer{}, err
	}
	details := map[string]string{"node": d.node.String(), "execution": d.execution.ID().String(), "target": d.target.String(), "reason": reason}
	if err := uc.rec.audit(ctx, "task.delegate", d.task.ID().String(), domain.AuditDenied, details); err != nil {
		return DelegationAnswer{}, err
	}
	return DelegationAnswer{Refused: &refusal}, nil
}

// runningExecutionOf is the execution the node holds and runs.
func runningExecutionOf(ctx context.Context, deps Dependencies, node domain.NodeID, id domain.ExecutionID, attempted string) (domain.Execution, error) {
	execution, err := deps.Executions.Get(ctx, id)
	if err != nil {
		return domain.Execution{}, err
	}
	if execution.NodeID() != node {
		return domain.Execution{}, domain.StateError{Subject: "execution " + id.String(), Current: "assigned to " + orNobody(execution.NodeID()), Attempted: attempted + " for node " + node.String()}
	}
	if execution.Outcome().IsFinal() {
		return domain.Execution{}, domain.StateError{Subject: "execution " + id.String(), Current: execution.Outcome().String(), Attempted: attempted}
	}
	return execution, nil
}

// answerFor describes a child as it is now.
func answerFor(child domain.Task, waitUntil time.Time) DelegationAnswer {
	return DelegationAnswer{TaskID: child.ID(), BotID: child.BotID(), Status: child.Status(), Done: child.Status().IsTerminal(), FailureReason: child.FailureReason(), WaitUntil: waitUntil}
}

func joinBots(ids []domain.BotID) string {
	if len(ids) == 0 {
		return "none"
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = id.String()
	}
	return strings.Join(names, ", ")
}
