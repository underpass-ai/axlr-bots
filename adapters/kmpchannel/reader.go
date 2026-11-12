package kmpchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// speaks MCP to it and calls kmp_wake on the key with a compact budget,
// without a model. It answers the wake's current state (newest first), its
// open loops and its next actions, one line each. A store that does not
// exist, or a key KMP does not know, is application.ErrContextNotFound;
// the read never makes a store.
type Reader struct {
	Binary   string
	Args     []string
	StateDir string
	// Timeout bounds one read; zero is DefaultTimeout.
	Timeout time.Duration
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
	var lines []string
	err := serve(ctx, r.Binary, r.Args, store, r.Timeout, "task-context", func(session *mcpSession) error {
		result, err := session.callToolResult("kmp_wake", map[string]any{"about": key.String(), "budget": map[string]any{"max_bytes": readBytes, "detail": "compact"}})
		if err != nil {
			return err
		}
		lines, err = wakeLines(result)
		return err
	})
	return lines, err
}

// wake is what kmp_wake's structured answer holds that a run reads, or
// the error's code.
type wake struct {
	Wake struct {
		CurrentState []string `json:"current_state"`
		OpenLoops    []string `json:"open_loops"`
		NextActions  []string `json:"next_actions"`
	} `json:"wake"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// wakeLines reads the wake: its state as KMP orders it, newest first,
// each line without the entry's reference, then the open loops and the
// next actions. A not_found error is ErrContextNotFound.
func wakeLines(result toolResult) ([]string, error) {
	var answer wake
	if len(result.StructuredContent) > 0 {
		if err := json.Unmarshal(result.StructuredContent, &answer); err != nil {
			return nil, fmt.Errorf("kmp_wake: unreadable answer: %w", err)
		}
	}
	if result.IsError {
		if answer.Error != nil && answer.Error.Code == "not_found" {
			return nil, application.ErrContextNotFound
		}
		text := ""
		if len(result.Content) > 0 {
			text = result.Content[0].Text
		}
		if answer.Error != nil {
			text = answer.Error.Code + ": " + answer.Error.Message
		}
		return nil, errors.New("kmp_wake refused: " + firstLine(text))
	}
	var lines []string
	for _, line := range answer.Wake.CurrentState {
		lines = append(lines, withoutRef(line))
	}
	for _, line := range answer.Wake.OpenLoops {
		lines = append(lines, "open: "+withoutRef(line))
	}
	for _, line := range answer.Wake.NextActions {
		lines = append(lines, "next: "+withoutRef(line))
	}
	return lines, nil
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
