package axlr

import (
	"os"
	"path/filepath"

	"github.com/underpass-ai/underpass-bots/domain"
)

// tracePath is where the execution's raw runtime lines are kept.
func (e *Executor) tracePath(execution domain.ExecutionID) string {
	return filepath.Join(e.StateDir, "traces", execution.String()+".jsonl")
}

// TracePath returns the execution's trace file when one exists on disk,
// so the control plane can keep it on an execution it ends by itself.
func (e *Executor) TracePath(execution domain.ExecutionID) (string, bool) {
	path := e.tracePath(execution)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}
