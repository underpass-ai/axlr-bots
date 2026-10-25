package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// approvalNotifier queues a delivery per target of the bot when a task
// starts to wait for a person. Queueing is idempotent: the delivery id
// is derived from the stopped execution and the target, so settling the
// same wait twice queues nothing new.
type approvalNotifier struct {
	deps Dependencies
	rec  recorder
}

// queue writes the deliveries; DeliverNotifications makes the attempts.
func (n approvalNotifier) queue(ctx context.Context, bot domain.Bot, task domain.Task, execution domain.Execution, wait domain.ApprovalWait) error {
	pending := execution.Result().PendingDecision
	if pending == nil {
		return nil
	}
	for _, target := range bot.Approvals().Targets() {
		id, err := domain.NewDeliveryID(deliveryID(execution.ID(), target.Name()))
		if err != nil {
			return err
		}
		if _, err := n.deps.Deliveries.Get(ctx, id); err == nil {
			continue
		} else if !domain.IsNotFound(err) {
			return err
		}
		payload, err := json.Marshal(n.payload(id, target, bot, task, execution, *pending, wait))
		if err != nil {
			return err
		}
		delivery, err := domain.NewDelivery(id, bot.ID(), task.ID(), execution.ID(), pending.CallID, target, payload, n.deps.Clock.Now())
		if err != nil {
			return err
		}
		if err := n.deps.Deliveries.Save(ctx, delivery); err != nil {
			return err
		}
		details := map[string]string{"delivery": id.String(), "task": task.ID().String(), "target": target.Name(), "kind": target.Kind().String(), "status": "queued"}
		if err := n.rec.audit(ctx, "approval.notify", id.String(), domain.AuditOK, details); err != nil {
			return err
		}
	}
	return nil
}

func (n approvalNotifier) payload(id domain.DeliveryID, target domain.NotifyTarget, bot domain.Bot, task domain.Task, execution domain.Execution, pending domain.PendingDecision, wait domain.ApprovalWait) ApprovalNotification {
	sessionID := pending.SessionID
	if sessionID == "" {
		sessionID = execution.Result().SessionID
	}
	call := ApprovalNotificationCall{Tool: pending.Tool, CallID: pending.CallID, SessionID: sessionID}
	if len(pending.Arguments) > MaxNotifiedArguments {
		call.ArgumentsOmitted = true
	} else {
		call.Arguments = pending.Arguments
	}
	taskPath := "/v1/tasks/" + url.PathEscape(task.ID().String())
	query := ""
	if pending.CallID != "" {
		query = "?call=" + url.QueryEscape(pending.CallID)
	}
	return ApprovalNotification{
		Schema: ApprovalNotificationSchema, DeliveryID: id.String(), Event: domain.EventTaskWaitingApproval.String(), Target: target.Name(),
		TenantID: bot.TenantID().String(), BotID: bot.ID().String(), BotName: bot.Name().String(), TaskID: task.ID().String(),
		ExecutionID: execution.ID().String(), CorrelationID: task.CorrelationID().String(), NodeID: execution.NodeID().String(),
		Objective: task.Objective().String(), Call: call, RequestedAt: wait.Since, Deadline: wait.Deadline, OnExpire: wait.OnExpire.String(),
		Correlation: domain.CorrelationRecord{BotID: bot.ID().String(), TaskID: task.ID().String(), ExecutionID: execution.ID().String(), TraceID: task.CorrelationID().String()},
		Actions: ApprovalActions{
			Approve: n.action("POST", taskPath+"/approve"+query),
			Deny:    n.action("POST", taskPath+"/deny"+query),
			Task:    n.action("GET", taskPath),
		},
	}
}

func (n approvalNotifier) action(method, path string) ApprovalAction {
	action := ApprovalAction{Method: method, Path: path}
	if base := strings.TrimRight(n.deps.OperatorURL, "/"); base != "" {
		action.URL = base + path
	}
	return action
}

// deliveryID is the delivery of the wait an execution stopped on to one
// target: the same on every queueing, so neither the outbox nor the
// receiver sees it twice.
func deliveryID(execution domain.ExecutionID, target string) string {
	sum := sha256.Sum256([]byte(execution.String() + "\x00" + target))
	return "dlv-" + hex.EncodeToString(sum[:12])
}
