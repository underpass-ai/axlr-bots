package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DefaultActivityHeartbeat is how long an idle watch waits before it tells
// that nothing changed.
const DefaultActivityHeartbeat = 15 * time.Second

// WatchTaskActivity follows the tree GetTaskActivity answers: first its
// snapshot, then one small update per change, until the caller cancels or
// the whole tree has finished. It keeps nothing per stream: each wake
// reads the tree and tells what is newer than the last cursor, so a watch
// resumed from a cursor needs no memory of the old one.
type WatchTaskActivity struct {
	get       GetTaskActivity
	changes   ChangeSignal
	heartbeat time.Duration
}

// NewWatchTaskActivity wires the use case; a zero heartbeat is
// DefaultActivityHeartbeat.
func NewWatchTaskActivity(deps Dependencies, changes ChangeSignal, heartbeat time.Duration) WatchTaskActivity {
	if heartbeat <= 0 {
		heartbeat = DefaultActivityHeartbeat
	}
	return WatchTaskActivity{get: NewGetTaskActivity(deps), changes: changes, heartbeat: heartbeat}
}

// Execute sends the updates through send until the tree finishes (nil),
// the caller's context ends (its error) or send fails (its error). A
// resume cursor that is not one of ours starts over with a snapshot.
func (uc WatchTaskActivity) Execute(ctx context.Context, id domain.TaskID, options ActivityOptions, resume string, send func(ActivityUpdate) error) error {
	since, resumed := parseActivityCursor(resume)
	first := true
	for {
		signal := uc.changes.Next()
		tree, err := uc.get.Execute(ctx, id, options)
		if err != nil {
			return err
		}
		hw := highWater(tree)
		var updates []ActivityUpdate
		switch {
		case first && !resumed:
			updates = []ActivityUpdate{{Cursor: activityCursor(hw), Snapshot: &tree}}
		default:
			updates = activityDelta(tree, since)
		}
		if first && resumed && len(updates) == 0 {
			at := since
			if hw.After(at) {
				at = hw
			}
			updates = []ActivityUpdate{{Cursor: activityCursor(at), Heartbeat: true}}
		}
		first = false
		for _, update := range updates {
			if err := send(update); err != nil {
				return err
			}
		}
		if hw.After(since) {
			since = hw
		}
		if finished(tree) {
			return nil
		}
		if len(updates) > 0 {
			// Read again at once: what was recorded while reading is told
			// by the next pass, or waited on with its signal.
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}
		timer := time.NewTimer(uc.heartbeat)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-signal:
			timer.Stop()
		case <-timer.C:
			if err := send(ActivityUpdate{Cursor: activityCursor(since), Heartbeat: true}); err != nil {
				return err
			}
		}
	}
}
