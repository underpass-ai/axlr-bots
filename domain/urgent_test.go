package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskPriorityAndReasons(t *testing.T) {
	if p, err := ParseTaskPriority(""); err != nil || p != PriorityNormal {
		t.Errorf("empty = %q %v", p, err)
	}
	if _, err := ParseTaskPriority("now"); err == nil {
		t.Error("an unknown priority was accepted")
	}
	task, _ := NewTask("task-1", "worker-1", "Roll back", "k-1", "trace-1", time.Unix(10, 0))
	if task.IsUrgent() || task.Snapshot().Priority != "" {
		t.Error("a new task is urgent")
	}
	task.Prioritize(PriorityUrgent)
	s := task.Snapshot()
	if !task.IsUrgent() || s.Priority != "urgent" {
		t.Errorf("priority = %q", s.Priority)
	}
	restored, err := RestoreTask(s)
	if err != nil || !restored.IsUrgent() || restored.Priority() != PriorityUrgent {
		t.Fatalf("restored = %v %v", restored.Priority(), err)
	}
	s.Priority = "soon"
	if _, err := RestoreTask(s); err == nil {
		t.Error("a bad priority was restored")
	}
	if err := restored.CancelBecause("preempted by task-2", time.Unix(20, 0)); err != nil || restored.FailureReason() != "preempted by task-2" || restored.Status() != TaskCancelled {
		t.Errorf("cancel = %v %+v", err, restored.Snapshot())
	}
	if err := restored.CancelBecause("again", time.Unix(30, 0)); err == nil {
		t.Error("a cancelled task was cancelled again")
	}
}

func TestGlobsAndToolRuleMatches(t *testing.T) {
	cases := []struct {
		pattern, text string
		match         bool
	}{
		{"*", "a", true}, {"*", "a/b", false}, {"**", "a/b", true}, {"a/*/c", "a/b/c", true}, {"a/**", "a/b/c", true},
		{"?", "x", true}, {"?", "/", false}, {"?", "", false}, {"run_*", "run_job", true}, {"x", "y", false}, {"", "", true}, {"", "a", false},
		{"*.json", "dir/x.json", false}, {"**.json", "dir/x.json", true},
	}
	for _, c := range cases {
		if got := globMatches(c.pattern, c.text); got != c.match {
			t.Errorf("%q ~ %q = %v", c.pattern, c.text, got)
		}
	}
	rule, _ := NewToolRule("rollback", nil)
	if !rule.Matches("rollback", nil) || rule.Matches("run_job", nil) {
		t.Error("a tool-only rule is wrong")
	}
	narrow, _ := NewToolRule("run_job", map[string]string{"name": "nightly-*", "workers": "5"})
	if !narrow.Matches("run_job", json.RawMessage(`{"name": "nightly-1", "workers": 5}`)) {
		t.Error("a matching call was refused")
	}
	for _, args := range []string{`{"name": "nightly-1"}`, `{"name": "backfill", "workers": 5}`, `not json`, `{"name": "nightly-1", "workers": 6}`} {
		if narrow.Matches("run_job", json.RawMessage(args)) {
			t.Errorf("%s matched", args)
		}
	}
}

func TestAutoApprove(t *testing.T) {
	approvals, err := NewApprovals(ApprovalsSnapshot{AutoApprove: []ToolRuleSnapshot{{Tool: "rollback"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !approvals.AutoApproves("rollback", []byte(`{"reason": "deploy"}`)) || approvals.AutoApproves("run_job", nil) {
		t.Error("direct calls are judged wrong")
	}
	wrapped := []byte(`{"name": "service/rollback", "why": "a failed deploy", "arguments": {"reason": "deploy"}}`)
	if !approvals.AutoApproves(MCPCallTool, wrapped) {
		t.Error("the wrapped MCP call was not approved")
	}
	if !approvals.AutoApproves(MCPCallTool, []byte(`{"name": "rollback"}`)) {
		t.Error("a wrapped call without arguments was not approved")
	}
	for _, bad := range []string{`{"name": "run_job", "arguments": {}}`, `{}`, `nope`} {
		if approvals.AutoApproves(MCPCallTool, []byte(bad)) {
			t.Errorf("%s approved", bad)
		}
	}
	if s := approvals.Snapshot(); len(s.AutoApprove) != 1 || s.AutoApprove[0].Tool != "rollback" {
		t.Errorf("snapshot = %+v", s)
	}
	if _, err := NewApprovals(ApprovalsSnapshot{AutoApprove: []ToolRuleSnapshot{{Tool: ""}}}); err == nil {
		t.Error("a nameless rule was accepted")
	}
	if _, err := NewApprovals(ApprovalsSnapshot{AutoApprove: make([]ToolRuleSnapshot, 17)}); err == nil {
		t.Error("17 rules were accepted")
	}
	if DefaultApprovals().AutoApproves("rollback", nil) {
		t.Error("the default approves a call")
	}
}

func TestWhatInterruptsABot(t *testing.T) {
	worker := teamTestBot(t, "worker-1", "default", []string{"ops"})
	team := implicitSubscription("warn.x")
	if !worker.InterruptedBy(team, KindStop) || !worker.InterruptedBy(team, KindDanger) || worker.InterruptedBy(team, KindWarn) || worker.InterruptedBy(team, KindInfo) {
		t.Error("the team's default is wrong")
	}
	loud, err := NewSubscription(SubscriptionSnapshot{Topic: "warn.*", Urgent: true})
	if err != nil || !loud.Urgent() || !loud.Snapshot().Urgent {
		t.Fatalf("loud = %+v %v", loud, err)
	}
	if !worker.InterruptedBy(loud, KindWarn) || worker.InterruptedBy(loud, KindInfo) {
		t.Error("an urgent subscription is wrong")
	}
	steady, _ := NewEventPolicy(EventsSnapshot{Interruptible: new(bool)})
	worker.SetEvents(steady)
	if worker.InterruptedBy(team, KindStop) || worker.InterruptedBy(loud, KindWarn) || worker.Events().Interruptible() {
		t.Error("interruptible false still interrupts")
	}
	if s := worker.Events().Snapshot(); s == nil || s.Interruptible == nil || *s.Interruptible {
		t.Errorf("snapshot = %+v", s)
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "manual", Interrupt: true}); err == nil {
		t.Error("a manual trigger took interrupt")
	}
}

func TestUnwrapCall(t *testing.T) {
	if tool, args, ok := UnwrapCall("local_read", []byte(`{"path": "x"}`)); !ok || tool != "local_read" || string(args) != `{"path": "x"}` {
		t.Errorf("direct = %s %s %v", tool, args, ok)
	}
	if tool, _, ok := UnwrapCall(MCPCallTool, []byte(`{"name": "axlr-bots/axlr_bots_emit_event", "arguments": {"name": "halt"}}`)); !ok || ToolBaseName(tool) != "axlr_bots_emit_event" {
		t.Errorf("wrapped = %s %v", tool, ok)
	}
	if _, _, ok := UnwrapCall(MCPCallTool, []byte(`{}`)); ok {
		t.Error("a call without a name unwrapped")
	}
}
