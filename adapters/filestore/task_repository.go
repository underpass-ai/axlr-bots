package filestore

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TaskRepository keeps tasks under <state>/tasks/<id>.json. The
// idempotency lookup and the listings by status scan the directory,
// reading only the files they do not know yet or that changed: enough
// for one server with thousands of tasks; an index comes with a real
// store.
type TaskRepository struct {
	files    collection
	statuses *statusStamps
	keys     *taskKeys
}

// NewTaskRepository stores tasks under the state directory.
func NewTaskRepository(stateDir string) TaskRepository {
	return TaskRepository{files: collection{dir: filepath.Join(stateDir, "tasks"), kind: "task"}, statuses: newStatusStamps(), keys: newTaskKeys()}
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

// FindByIdempotencyKey scans the bot's tasks for the key. A task's bot
// and key never change, so a task read once is matched on them without
// reading it again; the task found is read whole and checked.
func (r TaskRepository) FindByIdempotencyKey(ctx context.Context, bot domain.BotID, key domain.IdempotencyKey) (domain.Task, bool, error) {
	ids, err := r.files.ids()
	if err != nil {
		return domain.Task{}, false, err
	}
	wanted := taskKey{bot: bot, key: key}
	for _, id := range ids {
		if seen, ok := r.keys.get(id); ok && seen != wanted {
			continue
		}
		task, err := r.Get(ctx, domain.TaskID(id))
		if err != nil {
			return domain.Task{}, false, err
		}
		found := taskKey{bot: task.BotID(), key: task.IdempotencyKey()}
		r.keys.put(id, found)
		if found == wanted {
			return task, true, nil
		}
	}
	return domain.Task{}, false, nil
}

// ListByStatus answers the tasks in the status, in id order. A task
// whose file has not changed since a listing found it in another status
// is not read again.
func (r TaskRepository) ListByStatus(ctx context.Context, status domain.TaskStatus) ([]domain.Task, error) {
	entries, err := r.files.entries()
	if err != nil {
		return nil, err
	}
	tasks := make([]domain.Task, 0)
	for _, entry := range entries {
		if r.statuses.skip(entry, string(status)) {
			continue
		}
		task, err := r.Get(ctx, domain.TaskID(entry.id))
		if err != nil {
			return nil, err
		}
		r.statuses.remember(entry, string(task.Status()))
		if task.Status() == status {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// List reads every task, in the directory's order.
func (r TaskRepository) List(ctx context.Context) ([]domain.Task, error) {
	ids, err := r.files.ids()
	if err != nil {
		return nil, err
	}
	tasks := make([]domain.Task, 0, len(ids))
	for _, id := range ids {
		task, err := r.Get(ctx, domain.TaskID(id))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}
