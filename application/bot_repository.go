// Package application holds the control plane's ports and use cases. It
// depends on the domain only; adapters implement the ports.
package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// BotRepository stores bots durably: a saved bot survives a restart.
type BotRepository interface {
	Save(ctx context.Context, bot domain.Bot) error
	// Get returns a domain.NotFoundError when no bot has the id.
	Get(ctx context.Context, id domain.BotID) (domain.Bot, error)
	List(ctx context.Context) ([]domain.Bot, error)
}
