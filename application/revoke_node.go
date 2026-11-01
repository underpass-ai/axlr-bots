package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RevokeNode cuts a node off: its status becomes Revoked, its
// certificate's serial goes on the denylist so the next request it
// makes is refused, and the executions it still held are failed with
// their tasks, since nothing will report them. The node may be enrolled
// again with a fresh token.
type RevokeNode struct {
	deps Dependencies
	rec  recorder
}

// NewRevokeNode wires the use case.
func NewRevokeNode(deps Dependencies) RevokeNode {
	return RevokeNode{deps: deps, rec: recorder{deps: deps}}
}

// Execute revokes the node.
func (uc RevokeNode) Execute(ctx context.Context, id domain.NodeID) (RevokeNodeResult, error) {
	node, err := uc.deps.Nodes.Get(ctx, id)
	if err != nil {
		return RevokeNodeResult{}, err
	}
	now := uc.deps.Clock.Now()
	if err := node.Revoke(now); err != nil {
		return RevokeNodeResult{}, err
	}
	if serial := node.CertificateSerial(); serial != "" {
		if err := uc.deps.Denylist.Deny(ctx, serial, id.String()); err != nil {
			return RevokeNodeResult{}, err
		}
	}
	if err := uc.deps.Nodes.Save(ctx, node); err != nil {
		return RevokeNodeResult{}, err
	}
	failed, err := uc.failExecutions(ctx, node, now)
	if err != nil {
		return RevokeNodeResult{}, err
	}
	payload := map[string]any{"serial": node.CertificateSerial(), "failedTasks": taskIDs(failed)}
	if err := uc.rec.nodeEvent(ctx, domain.EventNodeRevoked, id, payload); err != nil {
		return RevokeNodeResult{}, err
	}
	details := map[string]string{"serial": node.CertificateSerial(), "failedTasks": joinTaskIDs(failed)}
	if err := uc.rec.audit(ctx, "node.revoke", id.String(), domain.AuditOK, details); err != nil {
		return RevokeNodeResult{}, err
	}
	return RevokeNodeResult{Node: node, FailedTasks: failed}, nil
}

// failExecutions ends every Running execution assigned to the node, and
// its task, as the reconciliation of a dead node would.
func (uc RevokeNode) failExecutions(ctx context.Context, node domain.Node, now time.Time) ([]domain.TaskID, error) {
	running, err := uc.deps.Executions.ListRunning(ctx)
	if err != nil {
		return nil, err
	}
	failed := []domain.TaskID{}
	reason := "revoked: node " + node.ID().String() + " was revoked at " + now.UTC().Format(time.RFC3339)
	for _, execution := range running {
		if execution.NodeID() != node.ID() {
			continue
		}
		if err := execution.Fail(reason, domain.Result{}, domain.Usage{}, "", now); err != nil {
			return nil, err
		}
		if err := uc.deps.Executions.Save(ctx, execution); err != nil {
			return nil, err
		}
		task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
		if err != nil {
			return nil, err
		}
		if task.Status() == domain.TaskRunning {
			if err := task.Fail(reason, now); err != nil {
				return nil, err
			}
			if err := uc.deps.Tasks.Save(ctx, task); err != nil {
				return nil, err
			}
		}
		bot, err := uc.deps.Bots.Get(ctx, task.BotID())
		if err != nil {
			return nil, err
		}
		payload := map[string]any{"executionId": execution.ID().String(), "reason": "revoked", "nodeId": node.ID().String()}
		if err := uc.rec.executionEvent(ctx, endEvent(task), bot, task.ID(), execution.ID(), task.CorrelationID(), payload); err != nil {
			return nil, err
		}
		if err := newIncidentAdvancer(uc.deps).taskEnded(ctx, bot, task, execution); err != nil {
			return nil, err
		}
		if err := newPlatformPublisher(uc.deps).taskEnded(ctx, bot, task, execution); err != nil {
			return nil, err
		}
		failed = append(failed, task.ID())
	}
	return failed, nil
}

func taskIDs(ids []domain.TaskID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func joinTaskIDs(ids []domain.TaskID) string {
	joined := ""
	for i, id := range ids {
		if i > 0 {
			joined += ","
		}
		joined += id.String()
	}
	return joined
}
