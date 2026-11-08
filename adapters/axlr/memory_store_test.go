package axlr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// A binary that takes --kmp-store gets each bot its own store under the
// node's state, on the execution and on the decision that continues it;
// an older binary gets nothing and resolves the store as before. On 8
// November 2026 every bot's memory had landed in the operator's per-user
// store, 1 093 sessions of it.
func TestExecuteGivesEachBotItsOwnMemoryStore(t *testing.T) {
	t.Setenv("FAKE_AXLR_KMP_STORE", "1")
	e, argsFile := fake(t, "result-completed")
	if _, err := e.Execute(context.Background(), request(0)); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(e.StateDir, "kmp", "sii")
	args := strings.Join(readArgs(t, argsFile), " ")
	if !strings.Contains(args, " --kmp-store "+store+" --root ") {
		t.Fatalf("args = %s", args)
	}
	if info, err := os.Stat(store); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("store %v %v", info, err)
	}
	other := request(0)
	other.ExecutionID, other.BotID = "exec-9", "triage"
	if _, err := e.Execute(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+filepath.Join(e.StateDir, "kmp", "triage")+" ") {
		t.Fatalf("args = %s", args)
	}
	if _, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-2", BotID: "sii", SessionID: "sess-1", CallID: "c2", Decision: "approve", Timeout: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+store+" ") {
		t.Fatalf("decision args = %s", args)
	}
}

func TestExecuteWithoutTheFlagLeavesTheStoreToTheRuntime(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	if _, err := e.Execute(context.Background(), request(0)); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); strings.Contains(args, "--kmp-store") {
		t.Fatalf("args = %s", args)
	}
	if _, err := os.Stat(filepath.Join(e.StateDir, "kmp")); !os.IsNotExist(err) {
		t.Fatalf("kmp directory made without the flag: %v", err)
	}
}

func TestMemoryStoreRefusesAStateItCannotWrite(t *testing.T) {
	t.Setenv("FAKE_AXLR_KMP_STORE", "1")
	e, _ := fake(t, "result-completed")
	blocked := filepath.Join(e.StateDir, "kmp")
	if err := os.WriteFile(blocked, []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(context.Background(), request(0)); err == nil || !strings.Contains(err.Error(), "memory store") {
		t.Fatalf("err = %v", err)
	}
}
