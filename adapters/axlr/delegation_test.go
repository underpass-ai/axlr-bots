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

func delegatingRequest() application.ExecutionRequest {
	r := request(time.Minute)
	r.AllowedTools = append(r.AllowedTools, application.DelegateToolName)
	r.Delegation = &application.DelegationOffer{Targets: []string{"bot-b"}, MaxDepth: 1}
	r.DelegationSocket = "/run/node/exec-1.sock"
	return r
}

func TestADelegatingRunLoadsTheRelayAndApprovesWhatThePolicyAllows(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	e.Relay = "/usr/local/bin/axlr-node"
	report, err := e.Execute(context.Background(), delegatingRequest())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(e.StateDir, "mcp", "exec-1.json")
	args := strings.Join(readArgs(t, argsFile), " ")
	if !strings.Contains(args, "--plugin "+manifestPath) || !strings.Contains(args, "--approve all") {
		t.Errorf("args = %s", args)
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest delegationManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.ManifestVersion != 1 || manifest.ID != "axlr-bots" || manifest.Command != "/usr/local/bin/axlr-node" ||
		strings.Join(manifest.Args, " ") != "mcp-relay --socket /run/node/exec-1.sock" || len(manifest.AllowTools) != 1 || manifest.AllowTools[0] != application.DelegateToolName {
		t.Errorf("manifest = %s (%v)", raw, err)
	}
	if info, _ := os.Stat(manifestPath); info.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode %v", info.Mode().Perm())
	}
	if report.Policy == nil || report.Policy.Delegation != manifestPath {
		t.Errorf("policy record = %+v", report.Policy)
	}
	policy, _ := os.ReadFile(filepath.Join(e.StateDir, "policies", "exec-1.json"))
	if !strings.Contains(string(policy), `"name": "`+application.DelegateToolName+`"`) {
		t.Errorf("the policy does not name the tool: %s", policy)
	}
	// A decision goes on with the tool.
	decide := application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-1", BotID: "sii", SessionID: "sess-1", Decision: "approve", Level: domain.AutonomyActWithApproval, Delegation: &application.DelegationOffer{Targets: []string{"bot-b"}}, DelegationSocket: "/run/node/exec-2.sock"}
	if _, err := e.Decide(context.Background(), decide); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, "--plugin "+filepath.Join(e.StateDir, "mcp", "exec-2.json")) {
		t.Errorf("decide args = %s", args)
	}
}

func TestTheDelegationToolIsNotWiredWithoutItsPieces(t *testing.T) {
	cases := map[string]func(*testing.T, *Executor, *application.ExecutionRequest){
		"no relay":  func(_ *testing.T, e *Executor, _ *application.ExecutionRequest) { e.Relay = "" },
		"no socket": func(_ *testing.T, _ *Executor, r *application.ExecutionRequest) { r.DelegationSocket = "" },
		"no offer":  func(_ *testing.T, _ *Executor, r *application.ExecutionRequest) { r.Delegation = nil },
		"no policy": func(t *testing.T, _ *Executor, _ *application.ExecutionRequest) { t.Setenv("FAKE_AXLR_NO_POLICY", "1") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			e, argsFile := fake(t, "result-completed")
			e.Relay = "/usr/local/bin/axlr-node"
			r := delegatingRequest()
			change(t, e, &r)
			report, err := e.Execute(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if args := strings.Join(readArgs(t, argsFile), " "); strings.Contains(args, "--plugin") || strings.Contains(args, "--approve all") {
				t.Errorf("args = %s", args)
			}
			if report.Policy.Delegation != "" {
				t.Errorf("policy record = %+v", report.Policy)
			}
		})
	}
}

func TestDelegationManifestErrors(t *testing.T) {
	e, _ := fake(t, "result-completed")
	e.Relay = "/usr/local/bin/axlr-node"
	if err := os.WriteFile(filepath.Join(e.StateDir, "mcp"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(context.Background(), delegatingRequest()); err == nil || !strings.Contains(err.Error(), "delegation manifest") {
		t.Errorf("execute: %v", err)
	}
	decide := application.DecisionRequest{ExecutionID: "exec-2", BotID: "sii", SessionID: "s", Decision: "deny", Level: domain.AutonomyObserve, Delegation: &application.DelegationOffer{Targets: []string{"bot-b"}}, DelegationSocket: "/x.sock"}
	if _, err := e.Decide(context.Background(), decide); err == nil {
		t.Error("decide wrote a manifest into a file")
	}
}
