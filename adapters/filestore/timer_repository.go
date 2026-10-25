package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TimerRepository keeps the timers under <state>/timers/<bot>~<trigger>.json:
// the next fire of every timer trigger, persisted before the daemon
// moves on, so a restart fires a missed slot once.
type TimerRepository struct {
	files collection
}

// NewTimerRepository stores timers under the state directory.
func NewTimerRepository(stateDir string) TimerRepository {
	return TimerRepository{files: collection{dir: filepath.Join(stateDir, "timers"), kind: "timer"}}
}

// Save writes the timer atomically.
func (r TimerRepository) Save(_ context.Context, timer domain.Timer) error {
	return r.files.write(timer.Key(), timer.Snapshot())
}

// Get reads and validates one timer.
func (r TimerRepository) Get(_ context.Context, key string) (domain.Timer, error) {
	var snapshot domain.TimerSnapshot
	if err := r.files.read(key, &snapshot); err != nil {
		return domain.Timer{}, err
	}
	return domain.RestoreTimer(snapshot)
}

// List reads every timer, in key order.
func (r TimerRepository) List(ctx context.Context) ([]domain.Timer, error) {
	keys, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	timers := make([]domain.Timer, 0, len(keys))
	for _, key := range keys {
		timer, err := r.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		timers = append(timers, timer)
	}
	return timers, nil
}

// Delete removes the timer's file; a missing one is not an error.
func (r TimerRepository) Delete(_ context.Context, key string) error {
	path, err := r.files.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
