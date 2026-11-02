// Package jsonl keeps append-only logs: one JSON object per line, each
// line fsynced before the call returns.
package jsonl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Modes of the logs: the events and the audit trail carry ids, prompts
// and actors, so only the daemon's user writes them and its group reads.
const (
	dirMode  os.FileMode = 0o750
	fileMode os.FileMode = 0o640
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
	if err := os.MkdirAll(filepath.Dir(a.path), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, fileMode)
	if err != nil {
		return err
	}
	line, err := afterTornTail(f, append(data, '\n'))
	if err != nil {
		_ = f.Close()
		return err
	}
	// One write: the line, with the newline a torn tail needs before it,
	// goes down in a single append.
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// afterTornTail puts a newline before the line when the file does not
// end in one: a crash in the middle of an append leaves a fragment, and
// the next line written straight after it would be glued to it and lost
// with it. The readers skip the fragment, now alone on its line.
func afterTornTail(f *os.File, line []byte) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return line, nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return nil, err
	}
	if last[0] == '\n' {
		return line, nil
	}
	return append([]byte{'\n'}, line...), nil
}
