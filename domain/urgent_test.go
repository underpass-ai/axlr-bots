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
	task, _ := NewTask("task-1", "quad-1", "Return home", "k-1", "trace-1", time.Unix(10, 0))
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
		{"?", "x", true}, {"?", "/", false}, {"?", "", false}, {"run_*", "run_mission", true}, {"x", "y", false}, {"", "", true}, {"", "a", false},
		{"*.json", "dir/x.json", false}, {"**.json", "dir/x.json", true},
	}
	for _, c := range cases {
		if got := globMatches(c.pattern, c.text); got != c.match {
			t.Errorf("%q ~ %q = %v", c.pattern, c.text, got)
		}
	}
	rule, _ := NewToolRule("return_home", nil)
	if !rule.Matches("return_home", nil) || rule.Matches("run_mission", nil) {
		t.Error("a tool-only rule is wrong")
	}
	narrow, _ := NewToolRule("run_mission", map[string]string{"name": "patrol-*", "speed_m_s": "5"})
	if !narrow.Matches("run_mission", json.RawMessage(`{"name": "patrol-1", "speed_m_s": 5}`)) {
		t.Error("a matching call was refused")
	}
	for _, args := range []string{`{"name": "patrol-1"}`, `{"name": "survey", "speed_m_s": 5}`, `not json`, `{"name": "patrol-1", "speed_m_s": 6}`} {
		if narrow.Matches("run_mission", json.RawMessage(args)) {
			t.Errorf("%s matched", args)
		}
	}
}

func TestAutoApprove(t *testing.T) {
	approvals, err := NewApprovals(ApprovalsSnapshot{AutoApprove: []ToolRuleSnapshot{{Tool: "return_home"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !approvals.AutoApproves("return_home", []byte(`{"reason": "ship"}`)) || approvals.AutoApproves("run_mission", nil) {
		t.Error("direct calls are judged wrong")
	}
	wrapped := []byte(`{"name": "vehicle/return_home", "why": "a ship", "arguments": {"reason": "ship"}}`)
	if !approvals.AutoApproves(MCPCallTool, wrapped) {
		t.Error("the wrapped MCP call was not approved")
	}
	if !approvals.AutoApproves(MCPCallTool, []byte(`{"name": "return_home"}`)) {
		t.Error("a wrapped call without arguments was not approved")
	}
	for _, bad := range []string{`{"name": "run_mission", "arguments": {}}`, `{}`, `nope`} {
		if approvals.AutoApproves(MCPCallTool, []byte(bad)) {
			t.Errorf("%s approved", bad)
		}
	}
	if s := approvals.Snapshot(); len(s.AutoApprove) != 1 || s.AutoApprove[0].Tool != "return_home" {
		t.Errorf("snapshot = %+v", s)
	}
	if _, err := NewApprovals(ApprovalsSnapshot{AutoApprove: []ToolRuleSnapshot{{Tool: ""}}}); err == nil {
		t.Error("a nameless rule was accepted")
	}
	if _, err := NewApprovals(ApprovalsSnapshot{AutoApprove: make([]ToolRuleSnapshot, 17)}); err == nil {
		t.Error("17 rules were accepted")
	}
	if DefaultApprovals().AutoApproves("return_home", nil) {
		t.Error("the default approves a call")
	}
}

func TestWhatInterruptsABot(t *testing.T) {
	quad := teamTestBot(t, "quad-1", "default", []string{"port"})
	team := implicitSubscription("warn.x")
	if !quad.InterruptedBy(team, KindStop) || !quad.InterruptedBy(team, KindDanger) || quad.InterruptedBy(team, KindWarn) || quad.InterruptedBy(team, KindInfo) {
		t.Error("the team's default is wrong")
	}
	loud, err := NewSubscription(SubscriptionSnapshot{Topic: "warn.*", Urgent: true})
	if err != nil || !loud.Urgent() || !loud.Snapshot().Urgent {
		t.Fatalf("loud = %+v %v", loud, err)
	}
	if !quad.InterruptedBy(loud, KindWarn) || quad.InterruptedBy(loud, KindInfo) {
		t.Error("an urgent subscription is wrong")
	}
	steady, _ := NewEventPolicy(EventsSnapshot{Interruptible: new(bool)})
	quad.SetEvents(steady)
	if quad.InterruptedBy(team, KindStop) || quad.InterruptedBy(loud, KindWarn) || quad.Events().Interruptible() {
		t.Error("interruptible false still interrupts")
	}
	if s := quad.Events().Snapshot(); s == nil || s.Interruptible == nil || *s.Interruptible {
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
	if tool, _, ok := UnwrapCall(MCPCallTool, []byte(`{"name": "axlr-bots/axlr_bots_emit_event", "arguments": {"name": "recall"}}`)); !ok || ToolBaseName(tool) != "axlr_bots_emit_event" {
		t.Errorf("wrapped = %s %v", tool, ok)
	}
	if _, _, ok := UnwrapCall(MCPCallTool, []byte(`{}`)); ok {
		t.Error("a call without a name unwrapped")
	}
}
