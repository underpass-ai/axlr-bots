package jsonl

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EventLog publishes events by appending them to <state>/events.jsonl.
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

// Path is the log file.
func (l *EventLog) Path() string { return l.file.path }
