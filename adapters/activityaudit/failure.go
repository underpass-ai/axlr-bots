package activityaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// The kinds of failure.
const (
	KindToolCall   = "tool_call"
	KindTaskFailed = "task_failed"
)

// The tools whose failures the signature reads more of: a local
// program's run and a plugin's tool behind AXLR's bridge.
const (
	toolLocalExec = "local_exec"
	toolCallTool  = "axlr_call_tool"
)

// Placeholders of what a failure's line did not show.
const (
	unknownValue = "?"
	// exitNonZero is a local_exec the runtime answered completed with an
	// exit code it cut: the program ran and did not exit 0.
	exitNonZero = "nonzero"
	// cutMarker is the error of a line cut before its error's words.
	cutMarker = "the run's line was cut before the error"
)

// Failure is one occurrence of a failure in a finished task: a call that
// ended failed, or the task that ended Failed. Error is normalised.
// Partial says the line was cut before what the signature needs, and the
// bot stands in for it. Recovered says the same call succeeded later in
// the same task.
type Failure struct {
	Kind      string
	Bot       string
	Team      string
	Task      string
	Tool      string
	Program   string
	Arg       string
	Exit      string
	MCPTool   string
	Error     string
	Partial   bool
	Recovered bool
	At        time.Time
}

// Key is what makes the failure's kind, as text: for a failed task its
// reason; for local_exec the program, its first argument, the exit code
// and the error; for a bridged call the plugin's tool and the error; for
// any other tool the tool and the error. The bot stands in for the
// program or the plugin's tool when the line did not show it.
func (f Failure) Key() string {
	parts := []string{f.Kind}
	if f.Kind == KindTaskFailed {
		return strings.Join(append(parts, "reason="+f.Error), "|")
	}
	parts = append(parts, f.Tool)
	switch {
	case f.Partial:
		parts = append(parts, "bot="+f.Bot)
	case f.Tool == toolLocalExec:
		parts = append(parts, "program="+f.Program, "arg="+f.Arg)
	case f.Tool == toolCallTool:
		parts = append(parts, "mcp="+f.MCPTool)
	}
	if f.Tool == toolLocalExec {
		parts = append(parts, "exit="+f.Exit)
	}
	return strings.Join(append(parts, "error="+f.Error), "|")
}

// ID is the signature's id: fallo- and the first 16 hex characters of
// the key's SHA-256.
func (f Failure) ID() string {
	sum := sha256.Sum256([]byte(f.Key()))
	return "fallo-" + hex.EncodeToString(sum[:8])
}
