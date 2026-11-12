package activityaudit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/internal/atomicfile"
)

// SeenFileName is the seen-store's file under the adapter's directory.
const SeenFileName = "seen.json"

// seenVersion is the file's version.
const seenVersion = 1

// SeenStore is the signatures the adapter has seen and the tasks it has
// audited, kept in its own file (SeenFileName), written atomically after
// each change. A store without a path keeps everything in memory: a dry
// run's.
type SeenStore struct {
	path       string
	cursor     time.Time
	signatures map[string]*SeenSignature
	tasks      map[string]time.Time
}

// OpenSeenStore reads the store at path; a file that is not there is an
// empty store. An empty path is a store in memory.
func OpenSeenStore(path string) (*SeenStore, error) {
	store := &SeenStore{path: path, signatures: map[string]*SeenSignature{}, tasks: map[string]time.Time{}}
	if path == "" {
		return store, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var file SeenFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if file.Version != seenVersion {
		return nil, fmt.Errorf("%s: version %d, this adapter reads %d", path, file.Version, seenVersion)
	}
	if file.Cursor != "" {
		if store.cursor, err = time.Parse(time.RFC3339Nano, file.Cursor); err != nil {
			return nil, fmt.Errorf("%s: cursor: %w", path, err)
		}
	}
	for i := range file.Signatures {
		signature := file.Signatures[i]
		store.signatures[signature.ID] = &signature
	}
	for _, task := range file.Tasks {
		created, err := time.Parse(time.RFC3339Nano, task.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("%s: task %s: %w", path, task.ID, err)
		}
		store.tasks[task.ID] = created
	}
	return store, nil
}

// Path is the store's file; empty in memory.
func (s *SeenStore) Path() string { return s.path }

// Cursor is the creation time the next reconciliation lists tasks from;
// zero when none was kept.
func (s *SeenStore) Cursor() time.Time { return s.cursor }

// SetCursor moves the cursor and forgets the audited tasks older than it,
// which no reconciliation lists again.
func (s *SeenStore) SetCursor(cursor time.Time) {
	s.cursor = cursor.UTC()
	for id, created := range s.tasks {
		if created.Before(s.cursor) {
			delete(s.tasks, id)
		}
	}
}

// Audited reports whether the task was audited.
func (s *SeenStore) Audited(task string) bool {
	_, ok := s.tasks[task]
	return ok
}

// MarkAudited records the task as audited.
func (s *SeenStore) MarkAudited(task string, created time.Time) { s.tasks[task] = created.UTC() }

// Observe counts one occurrence of the failure under its signature, a new
// one when the store has not seen it, and answers the signature.
func (s *SeenStore) Observe(f Failure) *SeenSignature {
	id := f.ID()
	at := stamp(f.At)
	signature, ok := s.signatures[id]
	if !ok {
		signature = &SeenSignature{ID: id, Key: f.Key(), Kind: f.Kind, Tool: f.Tool, Program: f.Program, McpTool: f.MCPTool, Error: f.Error, Partial: f.Partial, FirstAt: at, LastAt: at, FirstTask: f.Task, Bots: []string{}}
		s.signatures[id] = signature
	}
	signature.Count++
	if f.Recovered {
		signature.Recovered++
	}
	if at < signature.FirstAt || signature.FirstAt == "" {
		signature.FirstAt, signature.FirstTask = at, f.Task
	}
	if at >= signature.LastAt {
		signature.LastAt, signature.LastBot, signature.LastTask = at, f.Bot, f.Task
	}
	if !slices.Contains(signature.Bots, f.Bot) {
		signature.Bots = append(signature.Bots, f.Bot)
		slices.Sort(signature.Bots)
	}
	return signature
}

// Due reports whether the signature is to be published now: it has not
// been, and it is not one model's slip it fixed at once (an occurrence
// the same call did not recover from, or occurrences from two bots).
func Due(signature *SeenSignature) bool {
	if signature.PublishedAt != "" || signature.Count == 0 {
		return false
	}
	return signature.Count > signature.Recovered || len(signature.Bots) >= 2
}

// Pending answers the signatures due, oldest first.
func (s *SeenStore) Pending() []*SeenSignature {
	var due []*SeenSignature
	for _, signature := range s.signatures {
		if Due(signature) {
			due = append(due, signature)
		}
	}
	slices.SortFunc(due, func(a, b *SeenSignature) int {
		if a.FirstAt != b.FirstAt {
			if a.FirstAt < b.FirstAt {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return due
}

// Published records the event published for the signature.
func (s *SeenStore) Published(signature *SeenSignature, eventID string, at time.Time) {
	signature.PublishedAt, signature.EventID = stamp(at), eventID
}

// Rearm clears the signatures named (every one for "all"): each starts
// counting again, and its next occurrence publishes it once more under
// a new idempotency key. It answers the ids it knows, and an error naming
// the first it does not.
func (s *SeenStore) Rearm(ids []string) ([]string, error) {
	if slices.Contains(ids, "all") {
		ids = ids[:0]
		for id := range s.signatures {
			ids = append(ids, id)
		}
		slices.Sort(ids)
	}
	var rearmed []string
	for _, id := range ids {
		signature, ok := s.signatures[id]
		if !ok {
			return rearmed, fmt.Errorf("the seen-store has no signature %s", id)
		}
		signature.PublishedAt, signature.EventID, signature.Due = "", "", DuePublish{}
		signature.Count, signature.Recovered, signature.Bots = 0, 0, []string{}
		signature.Rearms++
		rearmed = append(rearmed, id)
	}
	return rearmed, nil
}

// Signatures answers every signature, the oldest first.
func (s *SeenStore) Signatures() []SeenSignature {
	all := make([]SeenSignature, 0, len(s.signatures))
	for _, signature := range s.signatures {
		all = append(all, *signature)
	}
	slices.SortFunc(all, func(a, b SeenSignature) int {
		switch {
		case a.FirstAt < b.FirstAt:
			return -1
		case a.FirstAt > b.FirstAt:
			return 1
		case a.ID < b.ID:
			return -1
		}
		return 1
	})
	return all
}

// IdempotencyKey is the publish's key of a signature: activity:<id>, and
// :r<n> after its n-th rearm.
func IdempotencyKey(signature *SeenSignature) string {
	if signature.Rearms == 0 {
		return "activity:" + signature.ID
	}
	return "activity:" + signature.ID + ":r" + strconv.Itoa(signature.Rearms)
}

// Save writes the store atomically; a store in memory writes nothing.
func (s *SeenStore) Save() error {
	if s.path == "" {
		return nil
	}
	file := SeenFile{Version: seenVersion, Signatures: s.Signatures(), Tasks: []AuditedTask{}}
	if !s.cursor.IsZero() {
		file.Cursor = stamp(s.cursor)
	}
	ids := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		file.Tasks = append(file.Tasks, AuditedTask{ID: id, CreatedAt: stamp(s.tasks[id])})
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.path, append(raw, '\n'), 0o600)
}

// stamp is a time as the store and the event write it: RFC 3339 in UTC,
// with nanoseconds, so that the text sorts as the time does.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
