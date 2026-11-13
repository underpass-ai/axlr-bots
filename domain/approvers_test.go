package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestApproversAreValidated(t *testing.T) {
	a, err := NewApprovals(ApprovalsSnapshot{Approvers: []string{"alice", " oncall "}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Approvers(), []string{"alice", "oncall"}) || !a.MayDecide("oncall") || !a.MayDecide("cli:alice") || !a.MayDecide("Alice") || a.MayDecide("cli:mallory") || a.MayDecide("") {
		t.Errorf("approvers = %v", a.Approvers())
	}
	s := a.Snapshot()
	if !reflect.DeepEqual(s.Approvers, []string{"alice", "oncall"}) {
		t.Errorf("snapshot = %+v", s)
	}
	again, err := NewApprovals(s)
	if err != nil || !reflect.DeepEqual(again.Snapshot(), s) {
		t.Errorf("round trip = %+v %v", again.Snapshot(), err)
	}
	kept, err := a.WithAutoApprove([]ToolRuleSnapshot{{Tool: "rollback"}})
	if err != nil || !reflect.DeepEqual(kept.Approvers(), a.Approvers()) {
		t.Errorf("autoApprove dropped the approvers: %+v %v", kept.Snapshot(), err)
	}
	none := DefaultApprovals()
	if !none.MayDecide("anyone") || none.Snapshot().Approvers != nil {
		t.Errorf("defaults = %+v", none.Snapshot())
	}
	many := make([]string, 33)
	for i := range many {
		many[i] = "p" + string(rune('a'+i%26)) + strings.Repeat("x", i)
	}
	for name, s := range map[string]ApprovalsSnapshot{
		"empty approver":    {Approvers: []string{" "}},
		"approver space":    {Approvers: []string{"alice smith"}},
		"approver repeated": {Approvers: []string{"alice", "alice"}},
		"same person twice": {Approvers: []string{"alice", "cli:Alice"}},
		"policy approver":   {Approvers: []string{"policy:autoApprove"}},
		"approver too long": {Approvers: []string{strings.Repeat("a", 129)}},
		"too many":          {Approvers: many},
	} {
		if _, err := NewApprovals(s); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestValidateActor(t *testing.T) {
	if got, err := ValidateActor("decidedBy", " cli:alice "); err != nil || got != "cli:alice" {
		t.Errorf("actor = %q %v", got, err)
	}
	for _, bad := range []string{"", "a b", "a\tb", "a\x7fb", strings.Repeat("a", 129)} {
		if _, err := ValidateActor("decidedBy", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// awaiting is a task waiting on a call.
func awaiting(t *testing.T) Task {
	t.Helper()
	task := sampleTask(t)
	if err := task.Start("exec-1", t0); err != nil {
		t.Fatal(err)
	}
	if err := task.AwaitApproval(ApprovalWait{CallID: "c-1", Tool: "local_write", Since: t0, Deadline: t0.Add(time.Hour), OnExpire: ExpireDeny}, t0); err != nil {
		t.Fatal(err)
	}
	return task
}

// What was stored before approvers reads back the same and writes back
// without the new fields.
func TestStoredJSONBeforeApproversRoundTrips(t *testing.T) {
	task := awaiting(t)
	raw, _ := json.Marshal(task.Snapshot())
	var snapshot TaskSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreTask(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(restored.Snapshot()); string(again) != string(raw) {
		t.Errorf("task round trip:\n%s\n%s", raw, again)
	}

	var old ApprovalsSnapshot
	if err := json.Unmarshal([]byte(`{"ttl":"15m","onExpire":"deny","autoApprove":[{"tool":"rollback"}]}`), &old); err != nil {
		t.Fatal(err)
	}
	approvals, err := NewApprovals(old)
	if err != nil || approvals.Approvers() != nil {
		t.Fatalf("old approvals: %+v %v", approvals.Snapshot(), err)
	}
	if out, _ := json.Marshal(approvals.Snapshot()); string(out) != `{"ttl":"15m","onExpire":"deny","autoApprove":[{"tool":"rollback"}]}` {
		t.Errorf("old approvals wrote %s", out)
	}
}

func TestActorKey(t *testing.T) {
	for _, same := range []string{"alice", "cli:alice", " Alice ", "CLI:alice"} {
		if ActorKey(same) != ActorKey("alice") {
			t.Errorf("%q is not alice", same)
		}
	}
	if ActorKey("cli:alice") == ActorKey("oncall") {
		t.Error("two people are one")
	}
}
