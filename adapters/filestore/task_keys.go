package filestore

import (
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// taskKey is what a task is found by on a retry: its bot and its
// idempotency key, which never change once the task is created.
type taskKey struct {
	bot domain.BotID
	key domain.IdempotencyKey
}

// taskKeys remembers each task's bot and key once read, so a lookup by
// key reads only the tasks it has not seen and the one it finds. Copies
// of a repository share it.
type taskKeys struct {
	mu   sync.Mutex
	byID map[string]taskKey
}

func newTaskKeys() *taskKeys { return &taskKeys{byID: map[string]taskKey{}} }

func (k *taskKeys) get(id string) (taskKey, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	key, ok := k.byID[id]
	return key, ok
}

func (k *taskKeys) put(id string, key taskKey) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.byID[id] = key
}
