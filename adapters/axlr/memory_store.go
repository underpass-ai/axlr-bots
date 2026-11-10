package axlr

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// kmpStoreFlag is the runtime's input for the KMP store a run opens:
// `axlr-tui --kmp-store <dir>`, which wins over the operator's mcp.json pin
// and the workspace (AXLR's docs/runtime-contract.md). Looked for in the
// binary's --help, as the other flags are.
const kmpStoreFlag = "-kmp-store"

// memoryTeamField is the policy's field for the team a run shares its
// memory with, as the help of --policy names it: a binary that reads it
// holds each member to its own about and the team's inside the team's
// store. Looked for in --help like the flags, since an older binary
// refuses a policy with a field it does not know.
const memoryTeamField = "memory.team"

// memoryTeam is the team whose store the bot's run opens: the bot's
// memory team when the binary takes both the store and the team, else
// none, and the run opens the bot's own store as before. A name with a
// dot is no team here: AXLR writes a member's about as
// project:<team>.<member>, so a dot in the team would read as another
// team's member, and the runtime refuses it.
func (e *Executor) memoryTeam(ctx context.Context, team domain.TeamName) string {
	if team == "" || strings.Contains(team.String(), ".") {
		return ""
	}
	if f := e.detectFeatures(ctx); !f.kmpStore || !f.memoryTeam || !f.policy {
		return ""
	}
	return team.String()
}

// memoryStore is the KMP store of a run on this host: the team's,
// <state>/kmp/<team>, for a bot that shares its memory with one
// (docs/architecture/memory-store-per-team.md), else the bot's own,
// <state>/kmp/<bot> (docs/architecture/memory-store-per-bot.md); never
// the operator's per-user store. A decision's run names the same
// directory as the execution that waits for it, so the session reopens
// the store it wrote.
func (e *Executor) memoryStore(bot domain.BotID, team string) string {
	if team != "" {
		return filepath.Join(e.StateDir, "kmp", team)
	}
	return filepath.Join(e.StateDir, "kmp", bot.String())
}

// memoryStoreArgs creates the run's store directory and names it to the
// run when the binary takes --kmp-store; an older binary gets nothing,
// and its runs resolve the store as before (the operator's).
func (e *Executor) memoryStoreArgs(ctx context.Context, bot domain.BotID, team string) ([]string, error) {
	if !e.detectFeatures(ctx).kmpStore {
		return nil, nil
	}
	store := e.memoryStore(bot, team)
	// Memories are the bot's and its team's alone, as the console keeps
	// a person's.
	if err := os.MkdirAll(store, 0o700); err != nil {
		return nil, err
	}
	return []string{"--kmp-store", store}, nil
}
