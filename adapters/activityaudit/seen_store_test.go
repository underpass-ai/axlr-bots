package activityaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A store that cannot be read is refused, never taken as empty: the
// next run would publish every signature again.
func TestABadStoreIsRefused(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"not json":     "{",
		"version":      `{"version":2,"signatures":[],"tasks":[]}`,
		"cursor":       `{"version":1,"cursor":"yesterday","signatures":[],"tasks":[]}`,
		"task created": `{"version":1,"signatures":[],"tasks":[{"id":"task-1","created_at":"soon"}]}`,
	} {
		path := filepath.Join(dir, name+".json")
		_ = os.WriteFile(path, []byte(content), 0o600)
		if _, err := OpenSeenStore(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := OpenSeenStore(dir); err == nil {
		t.Error("a directory was read as the store")
	}
	if err := (&SeenStore{path: filepath.Join(dir, "not json.json", "x"), signatures: map[string]*SeenSignature{}, tasks: map[string]time.Time{}}).Save(); err == nil {
		t.Error("saved under a file")
	}
}
