// Package kmpchannel writes the team channel's records into a team's KMP
// store on a node (docs/architecture/team-channel.md, "Memory"): it starts
// kmp-mcp on the team's store, speaks MCP to it and calls
// kmp_write_memory, without a model.
package kmpchannel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// DefaultTimeout bounds one write, the server's start included: a write
// took under 0.1 s on 10 November 2026.
const DefaultTimeout = 30 * time.Second

// Writer writes a record into <state>/kmp/<team>, the store the team's
// bots open on this node (docs/architecture/memory-store-per-team.md),
// under the about project:<team>.canal, as the actor kmp/<team>#canal:
// the channel's own agent, which no member can be (the control plane
// refuses a bot named canal) and whose about no member may write (AXLR
// holds a member to its own abouts and the team's). The about and the
// actor come from the record's team, never from the wire.
type Writer struct {
	// Binary is kmp-mcp; Args go before nothing else (a test's helper
	// process takes its own).
	Binary string
	Args   []string
	// StateDir is the node's state, where kmp/<team> lives.
	StateDir string
	// Timeout bounds one write; zero is DefaultTimeout.
	Timeout time.Duration
}

var _ application.ChannelMemoryWriter = Writer{}

// Actor is the channel's agent in a team's store, kmp/<team>#canal: the
// store as the answer names it, then the reserved bot.
func Actor(team domain.TeamName) string {
	return "kmp/" + team.String() + "#" + domain.ChannelBotID.String()
}

// Store is the team's store on this node.
func (w Writer) Store(team domain.TeamName) string {
	return filepath.Join(w.StateDir, "kmp", team.String())
}

// WriteChannelRecord writes the record. The same record again is the
// same write (KMP replays it under its idempotency key).
func (w Writer) WriteChannelRecord(ctx context.Context, record domain.ChannelRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	store := w.Store(record.Team)
	// The team's memories are its own, as the runs keep them.
	if err := os.MkdirAll(store, 0o700); err != nil {
		return err
	}
	return serve(ctx, w.Binary, w.Args, store, w.Timeout, "team-channel", func(session *mcpSession) error {
		_, err := session.callTool("kmp_write_memory", arguments(record))
		return err
	})
}

// serve starts kmp-mcp on the store, without a viewer, opens an MCP
// session on its stdio, hands it to call and stops the server, within
// the timeout (zero: DefaultTimeout). An error names the server's last
// line on stderr.
func serve(ctx context.Context, binary string, args []string, store string, timeout time.Duration, purpose string, call func(*mcpSession) error) error {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(withoutKMP(os.Environ()), "KMP_MCP_DATA_DIR="+store, "KMP_VIEWER_ADDR=off")
	cmd.WaitDelay = 2 * time.Second
	var stderr tail
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	session := newMCPSession(in, out)
	err = session.open("axlr-node", purpose)
	if err == nil {
		err = call(session)
	}
	_ = in.Close()
	waitErr := cmd.Wait()
	if err != nil && !errors.Is(err, application.ErrContextNotFound) {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w (%v)", err, ctx.Err())
		}
		if last := stderr.last(); last != "" {
			err = fmt.Errorf("%w; stderr: %s", err, last)
		}
	}
	if err != nil {
		return err
	}
	var exit *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exit) {
		return waitErr
	}
	return nil
}

// arguments are kmp_write_memory's: one observation under the channel's
// about, with the exchange as its evidence and its provenance as labels.
func arguments(record domain.ChannelRecord) map[string]any {
	labels := map[string][]string{
		"side": {record.Side.String()}, "team": {record.Other.String()}, "question": {record.Question.String()},
		"asker": {record.Asker.String()}, "responder": {record.Responder.String()}, "status": {strings.ToLower(record.Status.String())},
	}
	return map[string]any{
		"about": record.About(), "actor": Actor(record.Team), "idempotency_key": record.Key(),
		"memories": []map[string]any{{
			"id": "exchange", "kind": "observation", "summary": record.Summary(), "evidence": record.Evidence(), "labels": labels,
		}},
	}
}

// withoutKMP drops the store and viewer the node's own environment may
// name: the write opens the team's store and no viewer.
func withoutKMP(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "KMP_MCP_DATA_DIR=") || strings.HasPrefix(entry, "KMP_VIEWER_ADDR=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
