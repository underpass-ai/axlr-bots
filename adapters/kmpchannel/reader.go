package kmpchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// readBytes is the budget a wake is asked for. Under about 4 KiB a
// compact wake of KMP 0.26 answers only "…" (tried 12 November 2026), so
// the node asks for more and keeps MaxContextReadBytes of it.
const readBytes = 8192

// Reader reads what a team's store on this node holds under a KMP key,
// before a run that carries the key (docs/architecture/team-subscriptions.md,
// "Done is a contract", E8d): it starts kmp-mcp on <state>/kmp/<team>,
// speaks MCP to it and, without a model, calls kmp_time to rewind the key
// from now (its newest entries first, kind and text) and kmp_wake on it
// (its open loops and next actions), each with a compact budget. A store
// that does not exist, or a key KMP does not know, is
// application.ErrContextNotFound; the read never makes a store. A read
// KMP cut to nothing ("…") is an error, never an empty success.
type Reader struct {
	Binary   string
	Args     []string
	StateDir string
	// Timeout bounds one read; zero is DefaultTimeout.
	Timeout time.Duration
	// Now is the time the rewind starts from; nil is time.Now.
	Now func() time.Time
}

var _ application.ContextReader = Reader{}

// Store is the team's store on this node.
func (r Reader) Store(team domain.TeamName) string {
	return filepath.Join(r.StateDir, "kmp", team.String())
}

// ReadContext reads the key from the team's store.
func (r Reader) ReadContext(ctx context.Context, team domain.TeamName, key domain.ContextKey) ([]string, error) {
	if _, err := domain.NewTeamName(team.String()); err != nil {
		return nil, err
	}
	if _, err := domain.NewContextKey(key.String()); err != nil {
		return nil, err
	}
	store := r.Store(team)
	if info, err := os.Stat(store); err != nil || !info.IsDir() {
		return nil, application.ErrContextNotFound
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	budget := map[string]any{"max_bytes": readBytes, "detail": "compact"}
	var lines []string
	err := serve(ctx, r.Binary, r.Args, store, r.Timeout, "task-context", func(session *mcpSession) error {
		rewound, err := session.callToolResult("kmp_time", map[string]any{"about": key.String(), "move": "rewind", "from": map[string]any{"time": now().UTC().Format(time.RFC3339)}, "budget": budget})
		if err != nil {
			return err
		}
		entries, err := entryLines(rewound)
		if err != nil {
			return err
		}
		woken, err := session.callToolResult("kmp_wake", map[string]any{"about": key.String(), "budget": budget})
		if err != nil {
			return err
		}
		loops, err := wakeLines(woken)
		if err != nil {
			return err
		}
		lines = append(entries, loops...)
		if len(lines) == 0 && (cutTo(rewound) || cutTo(woken)) {
			return errors.New("KMP cut the read to nothing")
		}
		return nil
	})
	return lines, err
}

// entry is one entry kmp_time rewound to: its kind and its text.
type entry struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// answer is what kmp_time's and kmp_wake's structured answers hold that
// a run reads, or the error's code.
type answer struct {
	Entries []entry `json:"entries"`
	Wake    struct {
		CurrentState []string `json:"current_state"`
		OpenLoops    []string `json:"open_loops"`
		NextActions  []string `json:"next_actions"`
	} `json:"wake"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// read decodes a tool's structured answer; a not_found error is
// ErrContextNotFound, any other error the tool's.
func read(tool string, result toolResult) (answer, error) {
	var out answer
	if len(result.StructuredContent) > 0 {
		if err := json.Unmarshal(result.StructuredContent, &out); err != nil {
			return answer{}, fmt.Errorf("%s: unreadable answer: %w", tool, err)
		}
	}
	if !result.IsError {
		return out, nil
	}
	if out.Error != nil && out.Error.Code == "not_found" {
		return answer{}, application.ErrContextNotFound
	}
	text := ""
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}
	if out.Error != nil {
		text = out.Error.Code + ": " + out.Error.Message
	}
	return answer{}, errors.New(tool + " refused: " + firstLine(text))
}

// entryLines are the rewound entries, newest first, as "(kind) text";
// an entry KMP cut to "…" is left out.
func entryLines(result toolResult) ([]string, error) {
	got, err := read("kmp_time", result)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, entry := range got.Entries {
		if text := strings.TrimSpace(entry.Text); text != "" && text != "…" {
			lines = append(lines, "("+entry.Kind+") "+text)
		}
	}
	return lines, nil
}

// wakeLines are the wake's open loops and next actions, each without the
// entry's reference.
func wakeLines(result toolResult) ([]string, error) {
	got, err := read("kmp_wake", result)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range got.Wake.OpenLoops {
		if line = withoutRef(line); line != "…" {
			lines = append(lines, "open: "+line)
		}
	}
	for _, line := range got.Wake.NextActions {
		if line = withoutRef(line); line != "…" {
			lines = append(lines, "next: "+line)
		}
	}
	return lines, nil
}

// cutTo reports an answer whose state or entries KMP cut to "…".
func cutTo(result toolResult) bool {
	got, err := read("", result)
	if err != nil {
		return false
	}
	return slices.Contains(got.Wake.CurrentState, "…") || slices.ContainsFunc(got.Entries, func(e entry) bool { return strings.TrimSpace(e.Text) == "…" })
}

// withoutRef drops the entry's reference KMP puts before a line,
// "<ref> (<kind>): <text>", keeping the kind and the text.
func withoutRef(line string) string {
	ref, rest, ok := strings.Cut(line, " (")
	if !ok || strings.ContainsAny(ref, " \t") {
		return line
	}
	return "(" + rest
}
