// Package jsonl keeps append-only logs: one JSON object per line, each
// line fsynced before the call returns.
package jsonl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// appender appends JSON lines to one file.
type appender struct {
	path string
	mu   sync.Mutex
}

func (a *appender) append(record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
