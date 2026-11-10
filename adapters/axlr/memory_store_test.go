package axlr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
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

// policyMemoryOf reads the team the execution's policy file shares its
// memory with, "" when it names none.
func policyMemoryOf(t *testing.T, e *Executor, execution string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.StateDir, "policies", execution+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var file policyFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if file.Policy.Memory == nil {
		return ""
	}
	return file.Policy.Memory.Team
}

// The members of a team open one store, the team's, on the execution and
// on the decision that continues it, and each policy says whose it is, so
// the runtime holds a member to its own about and the team's. On 10
// November 2026 each QA bot had a store to itself, and qa-jefe's weekly
// plan never saw the charters qa-explorador recorded.
func TestExecuteGivesATeamsMembersOneMemoryStore(t *testing.T) {
	t.Setenv("FAKE_AXLR_KMP_STORE", "1")
	t.Setenv("FAKE_AXLR_MEMORY_TEAM", "1")
	e, argsFile := fake(t, "result-completed")
	e.MemoryTeams = []string{"qa", "qa.eu"}
	store := filepath.Join(e.StateDir, "kmp", "qa")
	for execution, bot := range map[string]string{"exec-1": "qa-jefe", "exec-2": "qa-explorador"} {
		member := request(0)
		member.ExecutionID, member.BotID, member.MemoryTeam = domain.ExecutionID(execution), domain.BotID(bot), "qa"
		if _, err := e.Execute(context.Background(), member); err != nil {
			t.Fatal(err)
		}
		if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+store+" --root "+filepath.Join(e.StateDir, "workspaces", bot)) {
			t.Fatalf("%s args = %s", bot, args)
		}
		if team := policyMemoryOf(t, e, execution); team != "qa" {
			t.Fatalf("%s policy shares memory with %q", bot, team)
		}
	}
	if info, err := os.Stat(store); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("store %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(e.StateDir, "kmp", "qa-jefe")); !os.IsNotExist(err) {
		t.Fatalf("a member of a team got a store of its own: %v", err)
	}
	if _, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-3", BotID: "qa-jefe", SessionID: "sess-1", CallID: "c2", Decision: "approve", Timeout: time.Minute, MemoryTeam: "qa"}); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+store+" ") || policyMemoryOf(t, e, "exec-3") != "qa" {
		t.Fatalf("decision args = %s", args)
	}
	// A bot in no team keeps its own store and a policy that names none.
	alone := request(0)
	alone.ExecutionID = "exec-4"
	if _, err := e.Execute(context.Background(), alone); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+filepath.Join(e.StateDir, "kmp", "sii")+" ") || policyMemoryOf(t, e, "exec-4") != "" {
		t.Fatalf("args = %s", args)
	}
	// A team's name with a dot would read as another team's member in
	// project:<team>.<member>: such a bot keeps its own store.
	dotted := request(0)
	dotted.ExecutionID, dotted.MemoryTeam = "exec-5", "qa.eu"
	if _, err := e.Execute(context.Background(), dotted); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+filepath.Join(e.StateDir, "kmp", "sii")+" ") || policyMemoryOf(t, e, "exec-5") != "" {
		t.Fatalf("args = %s", args)
	}
}

// A binary that does not read memory.team would refuse a policy that
// carries it and would hold no member to its own about: its bots keep a
// store each, as before.
func TestExecuteKeepsAStorePerBotUnderARuntimeWithoutMemoryTeams(t *testing.T) {
	t.Setenv("FAKE_AXLR_KMP_STORE", "1")
	e, argsFile := fake(t, "result-completed")
	e.MemoryTeams = []string{"*"}
	member := request(0)
	member.BotID, member.MemoryTeam = "qa-jefe", "qa"
	if _, err := e.Execute(context.Background(), member); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+filepath.Join(e.StateDir, "kmp", "qa-jefe")+" ") {
		t.Fatalf("args = %s", args)
	}
	if team := policyMemoryOf(t, e, member.ExecutionID.String()); team != "" {
		t.Fatalf("the policy names a team the runtime does not read: %q", team)
	}
	if _, err := os.Stat(filepath.Join(e.StateDir, "kmp", "qa")); !os.IsNotExist(err) {
		t.Fatalf("the team's store was made: %v", err)
	}
}

// A team's memory is turned on team by team: on a node that names qa,
// the bots of qa share a store and a bot of dev keeps its own; with no
// team named every bot keeps its own, as before; "*" is every team. On
// 10 November 2026 the node about to be installed would have moved the
// thirty bots of nine teams at once, the marketing bots into three
// stores by squad, with playbooks that still wrote project:underpass-*.
func TestANodeSharesOnlyTheMemoryOfTheTeamsItNames(t *testing.T) {
	t.Setenv("FAKE_AXLR_KMP_STORE", "1")
	t.Setenv("FAKE_AXLR_MEMORY_TEAM", "1")
	for name, tc := range map[string]struct {
		teams   []string
		qa, dev string
	}{
		"none named": {nil, "qa-jefe", "dev-go"},
		"qa named":   {[]string{"qa"}, "qa", "dev-go"},
		"every team": {[]string{"*"}, "qa", "dev"},
	} {
		t.Run(name, func(t *testing.T) {
			e, argsFile := fake(t, "result-completed")
			e.MemoryTeams = tc.teams
			for execution, run := range map[string][3]string{"exec-1": {"qa-jefe", "qa", tc.qa}, "exec-2": {"dev-go", "dev", tc.dev}} {
				member := request(0)
				member.ExecutionID, member.BotID, member.MemoryTeam = domain.ExecutionID(execution), domain.BotID(run[0]), domain.TeamName(run[1])
				if _, err := e.Execute(context.Background(), member); err != nil {
					t.Fatal(err)
				}
				if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, " --kmp-store "+filepath.Join(e.StateDir, "kmp", run[2])+" ") {
					t.Fatalf("%s args = %s", run[0], args)
				}
				want := ""
				if run[2] == run[1] {
					want = run[1]
				}
				if team := policyMemoryOf(t, e, execution); team != want {
					t.Fatalf("%s policy shares memory with %q, want %q", run[0], team, want)
				}
			}
		})
	}
}
