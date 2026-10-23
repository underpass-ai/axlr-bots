package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/underpass-bots/domain"
)

// TaskRepository keeps tasks under <state>/tasks/<id>.json. The
// idempotency lookup scans the directory: enough for Phase 0, where one
// server holds a handful of tasks; an index comes with a real store.
type TaskRepository struct {
	files collection
}

// NewTaskRepository stores tasks under the state directory.
func NewTaskRepository(stateDir string) TaskRepository {
	return TaskRepository{files: collection{dir: filepath.Join(stateDir, "tasks"), kind: "task"}}
}

// Save writes the task atomically.
func (r TaskRepository) Save(_ context.Context, task domain.Task) error {
	return r.files.write(task.ID().String(), task.Snapshot())
}

// Get reads and validates one task.
func (r TaskRepository) Get(_ context.Context, id domain.TaskID) (domain.Task, error) {
	var snapshot domain.TaskSnapshot
	if err := r.files.read(id.String(), &snapshot); err != nil {
		return domain.Task{}, err
	}
	return domain.RestoreTask(snapshot)
}

// FindByIdempotencyKey scans the bot's tasks for the key.
func (r TaskRepository) FindByIdempotencyKey(ctx context.Context, bot domain.BotID, key domain.IdempotencyKey) (domain.Task, bool, error) {
	ids, err := r.files.ids()
	if err != nil {
		return domain.Task{}, false, err
	}
	for _, id := range ids {
		task, err := r.Get(ctx, domain.TaskID(id))
		if err != nil {
			return domain.Task{}, false, err
		}
		if task.BotID() == bot && task.IdempotencyKey() == key {
			return task, true, nil
		}
	}
	return domain.Task{}, false, nil
}
