package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// auditsOf lists the audits of an action.
func auditsOf(h *harness, action string) []domain.AuditEntry {
	var out []domain.AuditEntry
	for _, audit := range h.log.audits {
		if audit.Action == action {
			out = append(out, audit)
		}
	}
	return out
}

// A bot that lists approvers takes decisions from them only, approve and
// deny alike, and from a named actor only; each refusal is audited with
// the actor and changes nothing.
func TestApproversRefuseAnActorTheyDoNotList(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{Approvers: []string{"alice", "oncall"}})
	ctx := context.Background()
	uc := NewApproveTask(h.deps)
	for _, c := range []struct{ decision, actor string }{
		{DecisionApprove, "mallory"}, {DecisionDeny, "mallory"}, {DecisionApprove, ""}, {DecisionDeny, ""}, {DecisionApprove, "cli:mallory"},
	} {
		_, err := uc.Execute(ctx, task.ID(), c.decision, "", c.actor)
		if !domain.IsForbidden(err) {
			t.Errorf("%s by %q: %v", c.decision, c.actor, err)
		}
	}
	refused := auditsOf(h, "approval.refused")
	if len(refused) != 5 || refused[0].Actor != "mallory" || refused[0].Details["actor"] != "mallory" || refused[1].Details["decision"] != DecisionDeny || refused[2].Details["actor"] != "" || refused[0].Outcome != domain.AuditDenied {
		t.Fatalf("refusals = %+v", refused)
	}
	if len(h.decider.requests) != 0 || len(h.log.events) != 0 {
		t.Fatal("a refused decision ran or published")
	}
	if _, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "bad actor"); err == nil {
		t.Error("an actor with a space was taken")
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}}
	res, err := uc.Execute(ctx, task.ID(), DecisionApprove, "", "oncall")
	if err != nil || res.Task.Status() != domain.TaskCompleted || len(h.decider.requests) != 1 {
		t.Fatalf("approve by an approver: %v %v", err, res.Task.Status())
	}
	decided, _ := lastOf(h, domain.EventTaskDecided)
	if decided.Payload["decidedBy"] != "oncall" {
		t.Errorf("decided = %+v", decided.Payload)
	}
	if audits := auditsOf(h, "task.decide"); len(audits) == 0 || audits[len(audits)-1].Actor != "oncall" {
		t.Errorf("the decision was audited as %+v", audits)
	}
}

// A bot without approvers decides as before: any actor, and none is the
// control plane's own.
func TestWithoutApproversAnyoneDecides(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{})
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}}
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionDeny, "", ""); err != nil {
		t.Fatal(err)
	}
	decided, _ := lastOf(h, domain.EventTaskDecided)
	if decided.Payload["decidedBy"] != "test:operator" {
		t.Errorf("decidedBy = %v", decided.Payload["decidedBy"])
	}
}

// Review of #139, P3: a person never decides as a policy, nor, on a
// daemon and where names count, as the control plane itself.
func TestNoPersonClaimsAPolicyOrTheDaemon(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{Approvers: []string{"alice", "oncall", "test:operator"}})
	ctx := context.Background()
	for _, name := range []string{"policy:autoApprove", "policy:team-event"} {
		if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, "", name); !domain.IsForbidden(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	daemon := h.deps
	daemon.Daemon = true
	if _, err := NewApproveTask(daemon).Execute(ctx, task.ID(), DecisionApprove, "", "test:operator"); !domain.IsForbidden(err) {
		t.Errorf("the daemon's own name: %v", err)
	}
	if refused := auditsOf(h, "approval.refused"); len(refused) != 3 {
		t.Errorf("refusals = %+v", refused)
	}
	if len(h.decider.requests) != 0 {
		t.Fatal("a claimed name ran the call")
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, "", "test:operator"); err != nil {
		t.Errorf("in process, the CLI's own actor decides: %v", err)
	}
}
