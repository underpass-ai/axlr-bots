package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// BotRepository keeps bots under <state>/bots/<id>.json.
type BotRepository struct {
	files collection
}

// NewBotRepository stores bots under the state directory.
func NewBotRepository(stateDir string) BotRepository {
	return BotRepository{files: collection{dir: filepath.Join(stateDir, "bots"), kind: "bot"}}
}

// Save writes the bot atomically.
func (r BotRepository) Save(_ context.Context, bot domain.Bot) error {
	return r.files.write(bot.ID().String(), bot.Snapshot())
}

// Get reads and validates one bot.
func (r BotRepository) Get(_ context.Context, id domain.BotID) (domain.Bot, error) {
	var snapshot domain.BotSnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.Bot{}, err
	}
	return domain.RestoreBot(snapshot)
}

// List reads every bot, in id order.
func (r BotRepository) List(ctx context.Context) ([]domain.Bot, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	bots := make([]domain.Bot, 0, len(ids))
	for _, id := range ids {
		bot, err := r.Get(ctx, domain.BotID(id))
		if err != nil {
			return nil, err
		}
		bots = append(bots, bot)
	}
	return bots, nil
}
