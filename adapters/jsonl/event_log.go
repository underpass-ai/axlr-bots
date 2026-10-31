package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EventLog publishes events by appending them to <state>/events.jsonl,
// and reads them back for a bot, a task or a team through an index of
// where their lines are, so a read does not decode the whole log.
type EventLog struct {
	file *appender
	// mu guards the index the reads share.
	mu    sync.Mutex
	index eventIndex
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

// eventBatch is the first capacity of a read's result.
const eventBatch = 64

// ListEvents answers the bot's newest limit events, oldest first.
func (l *EventLog) ListEvents(_ context.Context, bot domain.BotID, limit int) ([]domain.Event, error) {
	return l.tail(limit, func(x *eventIndex) []int32 { return x.byBot[bot] })
}

// ListTaskEvents answers the task's newest limit events, oldest first:
// its timeline.
func (l *EventLog) ListTaskEvents(_ context.Context, task domain.TaskID, limit int) ([]domain.Event, error) {
	return l.tail(limit, func(x *eventIndex) []int32 { return x.byTask[task] })
}

// ListTeamEvents answers the newest limit events of any of the bots, and
// of the tenant's team itself,
// oldest first: a team's timeline.
func (l *EventLog) ListTeamEvents(_ context.Context, tenant domain.TenantID, team domain.TeamName, bots []domain.BotID, limit int) ([]domain.Event, error) {
	return l.tail(limit, func(x *eventIndex) []int32 {
		lists := [][]int32{x.byTeam[teamKey{tenant: tenant, team: team.String()}]}
		for _, bot := range slices.Compact(slices.Sorted(slices.Values(bots))) {
			lists = append(lists, x.byBot[bot])
		}
		return merged(lists...)
	})
}

// tail brings the index up to the log's end and decodes the newest
// limit events of the lines found, oldest first. A line that is not an
// event is skipped: the log is append-only and a torn line must not
// hide the rest.
func (l *EventLog) tail(limit int, found func(*eventIndex) []int32) ([]domain.Event, error) {
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
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.index.refresh(f); err != nil {
		return nil, err
	}
	positions := found(&l.index)
	// The first capacity is a constant, never the caller's limit: the
	// slice grows only with the events the log holds.
	kept := make([]domain.Event, 0, eventBatch)
	var buf []byte
	for i := len(positions) - 1; i >= 0 && len(kept) < limit; i-- {
		line := l.index.lines[positions[i]]
		// A line's length comes from the file; the index keeps none past
		// maxEventLineBytes, and the buffer never grows past it either.
		n := min(int(line.len), maxEventLineBytes)
		buf = slices.Grow(buf[:0], n)[:n]
		if _, err := f.ReadAt(buf, line.at); err != nil {
			return nil, err
		}
		var event domain.Event
		if err := json.Unmarshal(buf, &event); err != nil {
			continue
		}
		kept = append(kept, event)
	}
	slices.Reverse(kept)
	return kept, nil
}

func (l *EventLog) Path() string { return l.file.path }
