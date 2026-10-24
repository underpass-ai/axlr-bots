package axlr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

// sreRules are the SRE bot's rules: local_exec narrowed to journalctl
// and systemctl status, with the write forms denied.
func sreRules(t *testing.T) ([]string, domain.ToolRules) {
	t.Helper()
	allowed := []string{"local_exec", "local_list", "local_read", "local_search"}
	tools, _ := domain.NewAllowedTools(allowed)
	journal, _ := domain.NewToolRule("local_exec", map[string]string{"program": "journalctl"})
	status, _ := domain.NewToolRule("local_exec", map[string]string{"program": "systemctl", "args": `["status"**`})
	vacuum, _ := domain.NewToolRule("local_exec", map[string]string{"program": "journalctl", "args": `**--vacuum**`})
	write, _ := domain.NewToolRule("local_write", nil)
	rules, err := domain.NewToolRules([]domain.ToolRule{journal, status}, []domain.ToolRule{vacuum, write}, tools)
	if err != nil {
		t.Fatal(err)
	}
	return allowed, rules
}

func TestRuntimeLevelTranslatesObserveWithCommands(t *testing.T) {
	cases := []struct {
		level   domain.AutonomyLevel
		allowed []string
		want    string
	}{
		{domain.AutonomyObserve, []string{"local_read"}, "observe"},
		{domain.AutonomyObserve, []string{"local_read", "local_exec"}, "act-within-policy"},
		{domain.AutonomyPropose, []string{"local_exec"}, "propose"},
		{domain.AutonomyActWithApproval, []string{"local_exec"}, "act-with-approval"},
		{domain.AutonomyActWithinPolicy, []string{"local_read"}, "act-within-policy"},
	}
	for _, tc := range cases {
		if got := runtimeLevel(tc.level, tc.allowed); got != tc.want {
			t.Errorf("%s %v = %s, want %s", tc.level, tc.allowed, got, tc.want)
		}
	}
	if approveForLevel("act-within-policy") != "local" || approveForLevel("propose") != "local" || approveForLevel("observe") != "none" || approveForLevel("act-with-approval") != "none" {
		t.Error("approveForLevel wrong")
	}
	if approveRank("none") != 0 || approveRank("local") != 1 || approveRank("all") != 2 || approveRank("odd") != 0 {
		t.Error("approveRank wrong")
	}
}

func TestRenderPolicyMergesRulesIntoTheAllowList(t *testing.T) {
	allowed, rules := sreRules(t)
	object := renderPolicy(domain.AutonomyObserve, allowed, rules)
	if object.Level != "act-within-policy" {
		t.Errorf("level = %s", object.Level)
	}
	names := make([]string, 0, len(object.Tools.Allow))
	for _, rule := range object.Tools.Allow {
		names = append(names, rule.Name)
	}
	if strings.Join(names, " ") != "local_list local_read local_search local_exec local_exec" {
		t.Errorf("allow = %v", names)
	}
	if object.Tools.Allow[3].Arguments["program"] != "journalctl" || object.Tools.Allow[4].Arguments["args"] != `["status"**` {
		t.Errorf("allow rules = %+v", object.Tools.Allow)
	}
	if len(object.Tools.Deny) != 2 || object.Tools.Deny[0].Arguments["args"] != `**--vacuum**` || object.Tools.Deny[1].Name != "local_write" || object.Tools.Deny[1].Arguments != nil {
		t.Errorf("deny = %+v", object.Tools.Deny)
	}
	// Without rules every allowed tool is allowed as a whole; an empty
	// allow-list stays an empty list, not null.
	plain := renderPolicy(domain.AutonomyActWithApproval, []string{"local_read"}, domain.ToolRules{})
	if plain.Level != "act-with-approval" || len(plain.Tools.Allow) != 1 || plain.Tools.Allow[0].Arguments != nil || plain.Tools.Deny != nil {
		t.Errorf("plain = %+v", plain)
	}
	raw, _ := json.Marshal(policyFile{Policy: renderPolicy(domain.AutonomyObserve, nil, domain.ToolRules{})})
	if string(raw) != `{"policy":{"level":"observe","tools":{"allow":[]}}}` {
		t.Errorf("empty policy = %s", raw)
	}
}

func TestExecutePassesThePolicyAndRaisesTheApproval(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	allowed, rules := sreRules(t)
	req := request(time.Minute)
	req.Level = domain.AutonomyObserve
	req.AllowedTools = allowed
	req.ToolRules = rules
	report, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	policyPath := filepath.Join(e.StateDir, "policies", "exec-1.json")
	if !strings.Contains(args, "--approve local ") || !strings.Contains(args, "--policy "+policyPath+" ") {
		t.Errorf("args = %s", args)
	}
	want := domain.PolicyRecord{Level: "observe", RuntimeLevel: "act-within-policy", Enforced: true, Path: policyPath}
	if report.Policy == nil || *report.Policy != want {
		t.Errorf("policy = %+v", report.Policy)
	}
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var file policyFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Policy.Level != "act-within-policy" || len(file.Policy.Tools.Allow) != 5 || len(file.Policy.Tools.Deny) != 2 {
		t.Errorf("policy file = %s", raw)
	}

	// act-with-approval keeps --approve none: every allowed write is a
	// person's decision, and the policy is still passed.
	e2, argsFile2 := fake(t, "result-needs-decision")
	req.Level = domain.AutonomyActWithApproval
	report, err = e2.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	args = strings.Join(readArgs(t, argsFile2), " ")
	if !strings.Contains(args, "--approve none ") || !strings.Contains(args, "--policy ") || report.Outcome != domain.ExecutionDecisionRequired || report.Policy.RuntimeLevel != "act-with-approval" {
		t.Errorf("args = %s report = %+v", args, report.Policy)
	}

	// An operator's higher approval level is not lowered by the policy.
	e3, argsFile3 := fake(t, "result-completed")
	e3.Approve = "all"
	if _, err := e3.Execute(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile3), " "); !strings.Contains(args, "--approve all ") {
		t.Errorf("args = %s", args)
	}

	// A level the domain does not know is refused before anything runs.
	req.Level = "yolo"
	if _, err := e.Execute(context.Background(), req); err == nil {
		t.Error("bad level accepted")
	}
}

func TestExecuteWithoutPolicySupportRecordsAndFailsClosed(t *testing.T) {
	t.Setenv("FAKE_AXLR_NO_POLICY", "1")
	e, argsFile := fake(t, "result-completed")
	allowed, rules := sreRules(t)
	req := request(time.Minute)
	req.AllowedTools = allowed
	req.ToolRules = rules
	report, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	if strings.Contains(args, "--policy") || !strings.Contains(args, "--approve none ") {
		t.Errorf("args = %s", args)
	}
	policyPath := filepath.Join(e.StateDir, "policies", "exec-1.json")
	if report.Policy == nil || report.Policy.Enforced || report.Policy.RuntimeLevel != "act-within-policy" || report.Policy.Path != policyPath {
		t.Errorf("policy = %+v", report.Policy)
	}
	if _, err := os.Stat(policyPath); err != nil {
		t.Error("policy file not kept as a record:", err)
	}
	// Detection runs once per executor.
	if _, err := e.Execute(context.Background(), req); err != nil || e.supportsPolicy(context.Background()) {
		t.Errorf("second run: %v", err)
	}
}

func TestDecidePassesThePolicy(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	allowed, rules := sreRules(t)
	report, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-9", BotID: "sii", SessionID: "sess-1", Decision: "approve", Level: domain.AutonomyActWithApproval, AllowedTools: allowed, ToolRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	policyPath := filepath.Join(e.StateDir, "policies", "exec-9.json")
	if !strings.Contains(args, "--approve none ") || !strings.Contains(args, "--policy "+policyPath+" ") {
		t.Errorf("args = %s", args)
	}
	if report.Policy == nil || report.Policy.Path != policyPath || report.Policy.RuntimeLevel != "act-with-approval" {
		t.Errorf("policy = %+v", report.Policy)
	}
	if _, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-9", BotID: "sii", SessionID: "sess-1", Decision: "approve", Level: "odd"}); err == nil {
		t.Error("bad level accepted")
	}
}

func TestCapabilities(t *testing.T) {
	e, _ := fake(t, "result-completed")
	if !e.SupportsPolicy(context.Background()) || e.Version(context.Background()) != "axlr-tui 0.14.0-dev" {
		t.Errorf("capabilities: policy %v version %q", e.SupportsPolicy(context.Background()), e.Version(context.Background()))
	}
	missing := NewExecutor("/no/such/axlr-tui", t.TempDir())
	if missing.SupportsPolicy(context.Background()) || missing.Version(context.Background()) != "" {
		t.Error("a missing binary has capabilities")
	}
}

func TestPolicyFileErrors(t *testing.T) {
	blocked := NewExecutor(os.Args[0], filepath.Join(t.TempDir(), "file"))
	if err := os.WriteFile(blocked.StateDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := blocked.Execute(context.Background(), request(time.Minute)); err == nil || !strings.Contains(err.Error(), "policy file") {
		t.Errorf("blocked state dir: %v", err)
	}
	if _, err := blocked.preparePolicy(context.Background(), "", domain.AutonomyObserve, nil, domain.ToolRules{}); err == nil {
		t.Error("empty execution accepted")
	}
}
