package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EventLog publishes events by appending them to <state>/events.jsonl,
// and reads them back for one bot.
type EventLog struct {
	file *appender
}

// NewEventLog writes to events.jsonl under the state directory.
func NewEventLog(stateDir string) *EventLog {
	return &EventLog{file: &appender{path: filepath.Join(stateDir, "events.jsonl")}}
}

// Publish validates and appends the event.
func (l *EventLog) Publish(_ context.Context, event domain.Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	return l.file.append(event)
}

// maxEventLineBytes bounds one event line when reading back.
const maxEventLineBytes = 4 << 20

// ListEvents scans the log for the bot's events and returns the newest
// limit of them, oldest first. A line that is not an event is skipped:
// the log is append-only and a torn last line must not hide the rest.
func (l *EventLog) ListEvents(_ context.Context, bot domain.BotID, limit int) ([]domain.Event, error) {
	f, err := os.Open(l.file.path)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if limit <= 0 {
		limit = 1
	}
	kept := make([]domain.Event, 0, min(limit, 256))
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxEventLineBytes)
	for scanner.Scan() {
		var event domain.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil || event.BotID != bot {
			continue
		}
		kept = append(kept, event)
		if len(kept) > limit {
			kept = kept[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return kept, nil
}

// Path is the log file.
func (l *EventLog) Path() string { return l.file.path }
