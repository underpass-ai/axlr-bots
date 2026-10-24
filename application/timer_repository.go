package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TimerRepository stores the timers durably: the next fire of every
// timer trigger, apart from the bot, so a restart fires a missed slot
// once and never twice.
type TimerRepository interface {
	Save(ctx context.Context, timer domain.Timer) error
	// Get returns a domain.NotFoundError when no timer has the key.
	Get(ctx context.Context, key string) (domain.Timer, error)
	List(ctx context.Context) ([]domain.Timer, error)
	// Delete removes a timer; a missing one is not an error.
	Delete(ctx context.Context, key string) error
}
