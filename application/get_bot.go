package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetBot reads one bot.
type GetBot struct {
	deps Dependencies
}

// NewGetBot wires the use case.
func NewGetBot(deps Dependencies) GetBot { return GetBot{deps: deps} }

// Execute returns the bot or a domain.NotFoundError.
func (uc GetBot) Execute(ctx context.Context, id domain.BotID) (domain.Bot, error) {
	return uc.deps.Bots.Get(ctx, id)
}
