package grpcapi

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// recheckEvery is how often a watch looks again when nothing woke it: a
// change the control plane records wakes it at once, so this is for what
// changes without one (the same pace as the HTTP long polls).
const recheckEvery = 5 * time.Second

// eventWatch is what the Watch RPCs wait on: the long polls' change
// signal of unixapi.Local, held for as long as the client stays.
type eventWatch struct {
	changed func() <-chan struct{}
}

// afterCursor sends every event read finds after the cursor, then each
// new one, until ctx ends or an error. The signal is taken before each
// read, so a change while reading is not missed.
func (w eventWatch) afterCursor(ctx context.Context, after string, read func(after string) ([]domain.Event, error), send func(domain.Event) error) error {
	for {
		changed := w.changed()
		events, err := read(after)
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := send(event); err != nil {
				return err
			}
			after = string(event.ID)
		}
		if len(events) > 0 {
			continue
		}
		if err := w.wait(ctx, changed); err != nil {
			return err
		}
	}
}

// newest sends the newest `replay` events of a timeline that has no
// cursor (a bot's), oldest first, and then each new one: it reads the
// newest window again after each change and sends what follows the last
// event sent. A window that no longer holds the last event sent (more
// new events than it spans) is sent whole.
func (w eventWatch) newest(ctx context.Context, replay int, read func(limit int) ([]domain.Event, error), send func(domain.Event) error) error {
	changed := w.changed()
	events, err := read(replay)
	if err != nil {
		return err
	}
	last, err := w.sendAll(events, send, "")
	if err != nil {
		return err
	}
	for {
		if err := w.wait(ctx, changed); err != nil {
			return err
		}
		changed = w.changed()
		window, err := read(application.MaxEventLimit)
		if err != nil {
			return err
		}
		fresh := window
		for i := len(window) - 1; i >= 0 && last != ""; i-- {
			if string(window[i].ID) == last {
				fresh = window[i+1:]
				break
			}
		}
		if last, err = w.sendAll(fresh, send, last); err != nil {
			return err
		}
	}
}

func (eventWatch) sendAll(events []domain.Event, send func(domain.Event) error, last string) (string, error) {
	for _, event := range events {
		if err := send(event); err != nil {
			return last, err
		}
		last = string(event.ID)
	}
	return last, nil
}

// wait returns when changed closes or recheckEvery passes; the error is
// the context's when the client has gone.
func (eventWatch) wait(ctx context.Context, changed <-chan struct{}) error {
	timer := time.NewTimer(recheckEvery)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-changed:
	case <-timer.C:
	}
	return nil
}
