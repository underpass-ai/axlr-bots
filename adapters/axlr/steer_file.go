package axlr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// steerFlag is the runtime's input for what a person writes to a run
// while it works (AXLR's --steer-file).
const steerFlag = "-steer-file"

// ErrSteerUnsupported says the binary takes no --steer-file: an
// operator's message cannot reach its runs.
var ErrSteerUnsupported = errors.New("axlr: this axlr-tui takes no --steer-file; messages do not reach the run")

// SupportsSteer reports whether the binary takes --steer-file.
func (e *Executor) SupportsSteer(ctx context.Context) bool { return e.detectFeatures(ctx).steer }

// steerPath is the file a run reads messages from:
// <state>/steer/<execution>.jsonl.
func (e *Executor) steerPath(execution domain.ExecutionID) string {
	return filepath.Join(e.StateDir, "steer", execution.String()+".jsonl")
}

// steerArgs points the run at its steer file when the binary takes one.
func (e *Executor) steerArgs(ctx context.Context, execution domain.ExecutionID) []string {
	if !e.SupportsSteer(ctx) {
		return nil
	}
	return []string{"--steer-file", e.steerPath(execution)}
}

// Steer hands an operator's messages to a running execution: one
// {"text": ...} line each, appended to its steer file, which the run
// reads between model steps.
func (e *Executor) Steer(ctx context.Context, execution domain.ExecutionID, texts []string) error {
	if !e.SupportsSteer(ctx) {
		return ErrSteerUnsupported
	}
	path := e.steerPath(execution)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, text := range texts {
		line, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return err
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("axlr: steer %s: %w", execution, err)
		}
	}
	return nil
}

// dropSteer removes an execution's steer file once its run ended.
func (e *Executor) dropSteer(execution domain.ExecutionID) {
	_ = os.Remove(e.steerPath(execution))
}
