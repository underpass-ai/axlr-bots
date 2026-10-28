package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// noteWindow is how far back a prompt's notes reach.
const noteWindow = 24 * time.Hour

// notesFor reads what the bot heard as notes in the last 24 hours: the
// newest MaxPromptNotes of its bot.event.heard of mode note.
func notesFor(ctx context.Context, deps Dependencies, bot domain.Bot) (domain.EventNotes, error) {
	events, err := deps.EventReader.ListEvents(ctx, bot.ID(), eventHistory)
	if err != nil {
		return domain.EventNotes{}, err
	}
	since := deps.Clock.Now().Add(-noteWindow)
	var notes []domain.EventNote
	for _, event := range events {
		if event.Type != domain.EventEventHeard || event.Payload["mode"] != domain.ModeNote.String() || event.CreatedAt.Before(since) {
			continue
		}
		from, _ := event.Payload["from"].(string)
		name, _ := event.Payload["name"].(string)
		message, _ := event.Payload["message"].(string)
		notes = append(notes, domain.EventNote{From: from, Name: name, Message: message, At: event.CreatedAt})
	}
	return domain.NewEventNotes(notes), nil
}

// keepNotes gives a task that starts the notes its prompt carries and
// keeps them on it; a task that keeps notes already keeps those.
func keepNotes(ctx context.Context, deps Dependencies, bot domain.Bot, task *domain.Task) error {
	if task.HasNotes() {
		return nil
	}
	notes, err := notesFor(ctx, deps, bot)
	if err != nil {
		return err
	}
	return task.KeepNotes(notes)
}
