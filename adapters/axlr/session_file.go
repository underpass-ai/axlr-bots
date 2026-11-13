package axlr

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/application"
)

// ReadSession reads the whole file AXLR keeps for a session,
// <AXLR state>/sessions/<id>.json: every message of it, uncut, the
// transcript the node hands to the control plane
// (docs/architecture/transcripts.md). The id is not trusted: only
// letters, digits, - and _ name a file; a link or anything but a regular
// file of at most application.MaxTranscriptBytes is refused.
func (e *Executor) ReadSession(id string) ([]byte, error) {
	if !sessionName(id) {
		return nil, fmt.Errorf("axlr: session id %q names no session file", id)
	}
	state := e.SessionState
	if state == "" {
		state = axlrState()
	}
	if state == "" {
		return nil, errors.New("axlr: AXLR's state directory cannot be told (no XDG_STATE_HOME or HOME)")
	}
	path := filepath.Join(state, "sessions", id+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("axlr: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("axlr: %s is not a regular file", path)
	}
	if info.Size() > application.MaxTranscriptBytes {
		return nil, fmt.Errorf("axlr: %s is over %d bytes", path, application.MaxTranscriptBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("axlr: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, application.MaxTranscriptBytes+1))
	if err != nil {
		return nil, fmt.Errorf("axlr: %w", err)
	}
	if len(content) > application.MaxTranscriptBytes {
		return nil, fmt.Errorf("axlr: %s grew over %d bytes", path, application.MaxTranscriptBytes)
	}
	return content, nil
}

// sessionName reports whether id is 1 to 64 letters, digits, - or _.
func sessionName(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
