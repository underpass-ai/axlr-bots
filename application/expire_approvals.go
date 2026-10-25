package application

import (
	"context"
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExpireApprovals carries out the deadline of every pending call nobody
// decided in time (vision §8.2, §14). The bot's approvals say what
// happens: deny (the default) hands the session a denial under a new
// execution, as a person's deny would, so the session goes on without
// the call; cancel ends the task Cancelled. A paused bot cannot carry a
// denial, so its expired task is cancelled. Each expiry publishes
// bot.approval.expired and is audited. The deadline is on the task, so
// a restart loses none: the first pass after it expires what came due
// while the control plane was down.
type ExpireApprovals struct {
	deps    Dependencies
	rec     recorder
	approve ApproveTask
}

// NewExpireApprovals wires the use case.
func NewExpireApprovals(deps Dependencies) ExpireApprovals {
	return ExpireApprovals{deps: deps, rec: recorder{deps: deps}, approve: NewApproveTask(deps)}
}

// Due lists the WaitingApproval tasks whose deadline has passed, and the
// ones whose expiry was recorded but whose denial is still to be
// carried. A task that waited before approvals had deadlines is given
// one: the bot's time to live from when it began to wait. A task that
// cannot be given one is left out and its error returned with the
// others, so one broken task does not hold up every expiry.
func (uc ExpireApprovals) Due(ctx context.Context) ([]domain.TaskID, error) {
	tasks, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskWaitingApproval)
	if err != nil {
		return nil, err
	}
	now := uc.deps.Clock.Now()
	due := []domain.TaskID{}
	var first error
	for _, task := range tasks {
		wait, ok := task.Approval()
		if ok && wait.Outcome == domain.ApprovalExpired {
			due = append(due, task.ID())
			continue
		}
		if !ok || !wait.Open() {
			if wait, err = uc.record(ctx, &task); err != nil {
				if first == nil {
					first = fmt.Errorf("give task %s a deadline: %w", task.ID(), err)
				}
				continue
			}
		}
		if now.After(wait.Deadline) {
			due = append(due, task.ID())
		}
	}
	return due, first
}

// record gives a task without a deadline the wait it is under.
func (uc ExpireApprovals) record(ctx context.Context, task *domain.Task) (domain.ApprovalWait, error) {
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return domain.ApprovalWait{}, err
	}
	wait := domain.ApprovalWait{Since: task.UpdatedAt(), Deadline: task.UpdatedAt().Add(bot.Approvals().TTL()), OnExpire: bot.Approvals().OnExpire()}
	if execution, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID()); err == nil && execution.Result().PendingDecision != nil {
		wait.CallID, wait.Tool = execution.Result().PendingDecision.CallID, execution.Result().PendingDecision.Tool
	}
	if err := task.RecordApproval(wait); err != nil {
		return domain.ApprovalWait{}, err
	}
	return wait, uc.deps.Tasks.Save(ctx, *task)
}

// Expire carries out the expiry of one task and says what it did; a
// task decided or cancelled meanwhile, or not yet due, is left alone
// (Action is empty).
func (uc ExpireApprovals) Expire(ctx context.Context, id domain.TaskID) (ExpiredApproval, error) {
	task, err := uc.deps.Tasks.Get(ctx, id)
	if err != nil {
		return ExpiredApproval{}, err
	}
	wait, ok := task.Approval()
	if task.Status() != domain.TaskWaitingApproval || !ok {
		return ExpiredApproval{TaskID: id, Task: task}, nil
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return ExpiredApproval{}, err
	}
	now := uc.deps.Clock.Now()
	action := wait.OnExpire
	if !bot.AcceptsTasks() {
		action = domain.ExpireCancel
	}
	if wait.Open() {
		if !now.After(wait.Deadline) {
			return ExpiredApproval{TaskID: id, Task: task}, nil
		}
		if err := task.ExpireApproval(now); err != nil {
			return ExpiredApproval{}, err
		}
		if action == domain.ExpireCancel {
			if err := task.Cancel(now); err != nil {
				return ExpiredApproval{}, err
			}
		}
		if err := uc.deps.Tasks.Save(ctx, task); err != nil {
			return ExpiredApproval{}, err
		}
		payload := map[string]any{"callId": wait.CallID, "tool": wait.Tool, "deadline": wait.Deadline, "onExpire": wait.OnExpire.String(), "action": action.String()}
		if action != wait.OnExpire {
			payload["reason"] = "bot is " + bot.Status().String()
		}
		if err := uc.rec.executionEvent(ctx, domain.EventApprovalExpired, bot, task.ID(), task.CurrentExecutionID(), task.CorrelationID(), payload); err != nil {
			return ExpiredApproval{}, err
		}
		details := map[string]string{"action": action.String(), "deadline": wait.Deadline.UTC().Format(time.RFC3339), "call": wait.CallID}
		if err := uc.rec.audit(ctx, "approval.expire", task.ID().String(), domain.AuditOK, details); err != nil {
			return ExpiredApproval{}, err
		}
	} else if action == domain.ExpireCancel {
		// The expiry was recorded; the bot was paused before its denial.
		if err := task.Cancel(now); err != nil {
			return ExpiredApproval{}, err
		}
		if err := uc.deps.Tasks.Save(ctx, task); err != nil {
			return ExpiredApproval{}, err
		}
	}
	if action == domain.ExpireCancel {
		reason := "approval expired at " + wait.Deadline.UTC().Format(time.RFC3339)
		if err := uc.rec.executionEvent(ctx, domain.EventTaskCancelled, bot, task.ID(), "", task.CorrelationID(), map[string]any{"reason": reason}); err != nil {
			return ExpiredApproval{}, err
		}
		return ExpiredApproval{TaskID: id, Action: action, Task: task}, nil
	}
	previous, err := uc.deps.Executions.Get(ctx, task.CurrentExecutionID())
	if err != nil {
		return ExpiredApproval{}, err
	}
	if previous.Result().PendingDecision == nil {
		return ExpiredApproval{}, domain.StateError{Subject: "execution " + previous.ID().String(), Current: previous.Outcome().String(), Attempted: "deny a pending call"}
	}
	result, err := uc.approve.resume(ctx, bot, task, previous, DecisionDeny, domain.ApprovalExpired, map[string]any{"reason": "approval expired"})
	if err != nil {
		return ExpiredApproval{}, err
	}
	execution := result.Execution
	return ExpiredApproval{TaskID: id, Action: action, Task: result.Task, Execution: &execution}, nil
}
