package axlr

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ladderRules is the allow rule a grant pins: the one write tool, held
// to the incident's namespace.
func ladderRules(t *testing.T) domain.ToolRules {
	t.Helper()
	bound, err := domain.NewToolRule("patch", map[string]string{"namespace": "drift-lab"})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := domain.NewAllowedTools([]string{"local_read", "patch"})
	if err != nil {
		t.Fatal(err)
	}
	rules, err := domain.NewToolRules([]domain.ToolRule{bound}, nil, tools)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

// The file a grant hands the runtime: the level, the runtime's read and
// the write held to its namespace, no bare rule that would widen it.
// docs/architecture/autonomy-ladder.md records that AXLR v0.16.0, given
// exactly this file with --approve all, ran the write with no card and
// refused the same tool in another namespace and any tool not listed.
func TestLadderPolicyFileRendering(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	req := request(0)
	req.Level = domain.AutonomyActWithinPolicy
	req.AllowedTools = []string{"local_read", "patch"}
	req.ToolRules = ladderRules(t)
	req.Unattended = true
	report, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(report.Policy.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strings.Fields(string(data)), "")
	want := `{"policy":{"level":"act-within-policy","tools":{"allow":[{"name":"local_read"},{"name":"patch","arguments":{"namespace":"drift-lab"}}]}}}`
	if got != want {
		t.Fatalf("policy file:\n%s\nwant\n%s", got, want)
	}
	args := strings.Join(readArgs(t, argsFile), "\n") + "\n"
	if !strings.Contains(args, "--approve\nall\n") || !strings.Contains(args, "--policy") {
		t.Fatalf("args:\n%s", args)
	}
}

// Only a grant raises the run to --approve all: a bot at
// act-within-policy with no ladder keeps the level it had.
func TestOnlyAGrantRaisesTheApprovalLevel(t *testing.T) {
	for name, c := range map[string]struct {
		level      domain.AutonomyLevel
		unattended bool
		want       string
	}{
		"no grant at the top": {domain.AutonomyActWithinPolicy, false, "--approve\nlocal\n"},
		"grant at the card":   {domain.AutonomyActWithApproval, true, "--approve\nnone\n"},
		"grant at the top":    {domain.AutonomyActWithinPolicy, true, "--approve\nall\n"},
	} {
		t.Run(name, func(t *testing.T) {
			e, argsFile := fake(t, "result-completed")
			req := request(0)
			req.Level, req.Unattended, req.AllowedTools = c.level, c.unattended, []string{"local_read", "patch"}
			if _, err := e.Execute(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if args := strings.Join(readArgs(t, argsFile), "\n") + "\n"; !strings.Contains(args, c.want) {
				t.Fatalf("args:\n%s", args)
			}
		})
	}
}

// A runtime that cannot enforce the policy takes no grant: every call
// keeps its decision.
func TestAGrantNeedsAnEnforcedPolicy(t *testing.T) {
	p := preparedPolicy{approve: "none", object: policyObject{Level: "act-within-policy"}}
	p.actAlone(true)
	if p.approve != "none" {
		t.Fatalf("approve = %s", p.approve)
	}
}
