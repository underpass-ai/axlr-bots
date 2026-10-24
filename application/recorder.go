package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// recorder publishes events and audit entries for the use cases.
type recorder struct {
	deps Dependencies
}

func (r recorder) event(ctx context.Context, kind domain.EventType, bot domain.Bot, task domain.TaskID, correlation domain.CorrelationID, payload map[string]any) error {
	id, err := domain.NewEventID(r.deps.IDs.NewID("evt-"))
	if err != nil {
		return err
	}
	event, err := domain.NewEvent(id, kind, bot.TenantID(), bot.ID(), task, correlation, r.deps.Clock.Now(), payload)
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
