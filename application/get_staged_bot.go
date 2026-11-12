package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetStagedBot reads the change a bot holds staged; a bot that holds
// none is a StateError, as an apply of it is.
type GetStagedBot struct {
	deps Dependencies
}

// NewGetStagedBot wires the use case.
func NewGetStagedBot(deps Dependencies) GetStagedBot {
	return GetStagedBot{deps: deps}
}

// Execute answers the staged change, its secrets redacted.
func (uc GetStagedBot) Execute(ctx context.Context, id domain.BotID) (StagedBotView, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return StagedBotView{}, err
	}
	staged, ok := bot.Staged()
	if !ok {
		return StagedBotView{}, nothingStaged(id, "show a staged change")
	}
	return newStagedBotView(staged)
}
