package nats

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// topicTap decorates an application.EventPublisher: the event goes to
// the publisher it wraps first, and once that recorded it, its copies
// for the teams it was told to go to the topics' outbox. A copy that
// cannot be written is logged and never fails the event: the record is
// the source of truth, the topic a copy of it.
type topicTap struct {
	inner  application.EventPublisher
	topics *Topics
}

// Tap wraps the event publisher the use cases publish through.
func (t *Topics) Tap(inner application.EventPublisher) application.EventPublisher {
	return topicTap{inner: inner, topics: t}
}

// Publish hands the event on, then copies it to the team's topics.
func (p topicTap) Publish(ctx context.Context, event domain.Event) error {
	if err := p.inner.Publish(ctx, event); err != nil {
		return err
	}
	if err := p.topics.publish(event); err != nil {
		p.topics.cfg.log().Printf("bus: nats: topics: event %s not copied: %v", event.ID, err)
	}
	return nil
}
