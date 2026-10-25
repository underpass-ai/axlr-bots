package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// collection is one directory of <id>.json files for one aggregate kind.
type collection struct {
	dir  string
	kind string
}

// path is the file of the record id. The ids reaching here are
// validated identifiers; an id that is not a plain file name (a
// separator, a "..") is refused all the same, so no caller can step out
// of the directory.
func (c collection) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("filestore: %s id %q is not a file name", c.kind, id)
	}
	return filepath.Join(c.dir, id+".json"), nil
}

func (c collection) write(id string, snapshot any) error {
	path, err := c.path(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomically(path, append(data, '\n'))
}

func (c collection) read(id string, snapshot any) error {
	path, err := c.path(id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.NotFoundError{Kind: c.kind, ID: id}
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, snapshot); err != nil {
		return fmt.Errorf("filestore: %s %s is not valid JSON: %w", c.kind, id, err)
	}
	return nil
}

// ids lists the identifiers stored, in file-name order.
func (c collection) ids() ([]string, error) {
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	return ids, nil
}
