package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListBots reads every registered bot.
type ListBots struct {
	deps Dependencies
}

// NewListBots wires the use case.
func NewListBots(deps Dependencies) ListBots { return ListBots{deps: deps} }

// Execute returns the bots the repository holds.
func (uc ListBots) Execute(ctx context.Context) ([]domain.Bot, error) {
	return uc.deps.Bots.List(ctx)
}
