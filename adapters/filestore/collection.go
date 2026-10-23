package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/underpass-bots/domain"
)

// collection is one directory of <id>.json files for one aggregate kind.
type collection struct {
	dir  string
	kind string
}

func (c collection) path(id string) string {
	return filepath.Join(c.dir, id+".json")
}

func (c collection) write(id string, snapshot any) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomically(c.path(id), append(data, '\n'))
}

func (c collection) read(id string, snapshot any) error {
	data, err := os.ReadFile(c.path(id))
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
