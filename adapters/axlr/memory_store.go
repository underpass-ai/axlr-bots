package axlr

import (
	"context"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// kmpStoreFlag is the runtime's input for the KMP store a run opens:
// `axlr-tui --kmp-store <dir>`, which wins over the operator's mcp.json pin
// and the workspace (AXLR's docs/runtime-contract.md). Looked for in the
// binary's --help, as the other flags are.
const kmpStoreFlag = "-kmp-store"

// memoryStore is the bot's own KMP store on this host,
// <state>/kmp/<bot>: one per bot, beside its workspace, and never the
// operator's per-user store (docs/architecture/memory-store-per-bot.md).
// A decision's run names the same directory as the execution that waits
// for it, so the session reopens the store it wrote.
func (e *Executor) memoryStore(bot domain.BotID) string {
	return filepath.Join(e.StateDir, "kmp", bot.String())
}

// memoryStoreArgs creates the bot's store directory and names it to the
// run when the binary takes --kmp-store; an older binary gets nothing,
// and its runs resolve the store as before (the operator's).
func (e *Executor) memoryStoreArgs(ctx context.Context, bot domain.BotID) ([]string, error) {
	if !e.detectFeatures(ctx).kmpStore {
		return nil, nil
	}
	store := e.memoryStore(bot)
	// Memories are the bot's alone, as the console keeps a person's.
	if err := os.MkdirAll(store, 0o700); err != nil {
		return nil, err
	}
	return []string{"--kmp-store", store}, nil
}
