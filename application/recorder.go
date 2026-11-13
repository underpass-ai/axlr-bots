package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// recorder publishes events and audit entries for the use cases.
type recorder struct {
	deps Dependencies
}

func (r recorder) event(ctx context.Context, kind domain.EventType, bot domain.Bot, task domain.TaskID, correlation domain.CorrelationID, payload map[string]any) error {
	return r.executionEvent(ctx, kind, bot, task, "", correlation, payload)
}

// executionEvent publishes an event about an execution: the event names
// the bot, the task, the execution and the trace, the ids an execution
// is reconstructed by (vision §14.8).
func (r recorder) executionEvent(ctx context.Context, kind domain.EventType, bot domain.Bot, task domain.TaskID, execution domain.ExecutionID, correlation domain.CorrelationID, payload map[string]any) error {
	_, err := r.executionEventID(ctx, kind, bot, task, execution, correlation, payload)
	return err
}

// executionEventID publishes an event about an execution and answers
// its id.
func (r recorder) executionEventID(ctx context.Context, kind domain.EventType, bot domain.Bot, task domain.TaskID, execution domain.ExecutionID, correlation domain.CorrelationID, payload map[string]any) (domain.EventID, error) {
	id, err := domain.NewEventID(r.deps.IDs.NewID("evt-"))
	if err != nil {
		return "", err
	}
	event, err := domain.NewEvent(id, kind, bot.TenantID(), bot.ID(), task, correlation, r.deps.Clock.Now(), payload)
	if err != nil {
		return "", err
	}
	event.ExecutionID = execution
	return id, r.deps.Events.Publish(ctx, event)
}

// groupEvent publishes an event about a team.
func (r recorder) groupEvent(ctx context.Context, kind domain.EventType, tenant domain.TenantID, team domain.TeamName, payload map[string]any) error {
	id, err := domain.NewEventID(r.deps.IDs.NewID("evt-"))
	if err != nil {
		return err
	}
	event, err := domain.NewGroupEvent(id, kind, tenant, team, r.deps.Clock.Now(), payload)
	if err != nil {
		return err
	}
	return r.deps.Events.Publish(ctx, event)
}

// nodeEvent publishes an event about a node.
func (r recorder) nodeEvent(ctx context.Context, kind domain.EventType, node domain.NodeID, payload map[string]any) error {
	id, err := domain.NewEventID(r.deps.IDs.NewID("evt-"))
	if err != nil {
		return err
	}
	event, err := domain.NewNodeEvent(id, kind, node, r.deps.Clock.Now(), payload)
	if err != nil {
		return err
	}
	return r.deps.Events.Publish(ctx, event)
}

func (r recorder) audit(ctx context.Context, action, subject string, outcome domain.AuditOutcome, details map[string]string) error {
	entry, err := domain.NewAuditEntry(r.deps.IDs.NewID("aud-"), r.deps.Clock.Now(), r.deps.Actor, action, subject, outcome, details)
	if err != nil {
		return err
	}
	return r.deps.Audit.Record(ctx, entry)
}
