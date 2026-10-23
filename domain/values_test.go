package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIdentifiersAcceptAndReject(t *testing.T) {
	good := []string{"server-incident-investigator", "a", "x.y_z-1", strings.Repeat("a", 64), "  padded  "}
	bad := []string{"", "   ", "Upper", "with space", "ünï", strings.Repeat("a", 65)}
	for _, raw := range good {
		if _, err := NewBotID(raw); err != nil {
			t.Errorf("NewBotID(%q): %v", raw, err)
		}
	}
	for _, raw := range bad {
		if _, err := NewBotID(raw); err == nil {
			t.Errorf("NewBotID(%q) accepted", raw)
		}
	}
	for name, ctor := range map[string]func(string) error{
		"tenant":    func(s string) error { _, err := NewTenantID(s); return err },
		"task":      func(s string) error { _, err := NewTaskID(s); return err },
		"execution": func(s string) error { _, err := NewExecutionID(s); return err },
		"event":     func(s string) error { _, err := NewEventID(s); return err },
	} {
		if err := ctor("ok-1"); err != nil {
			t.Errorf("%s ok-1: %v", name, err)
		}
		if err := ctor("Bad Id"); err == nil {
			t.Errorf("%s accepted a bad id", name)
		}
	}
	id, _ := NewBotID("  b1 ")
	if id.String() != "b1" {
		t.Errorf("trimmed id = %q", id)
	}
	for _, s := range []fmt_stringer{TenantID("t"), TaskID("k"), ExecutionID("e"), EventID("v")} {
		if s.String() == "" {
			t.Error("empty String()")
		}
	}
}

type fmt_stringer interface{ String() string }

func TestValidationErrorMessage(t *testing.T) {
	_, err := NewBotID("")
	var verr ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want ValidationError, got %T", err)
	}
	if verr.Field != "bot id" || !strings.Contains(err.Error(), "bot id: must not be empty") {
		t.Errorf("unexpected error %q", err)
	}
}

func TestCorrelationID(t *testing.T) {
	if id, err := NewCorrelationID("  "); err != nil || id != "" {
		t.Errorf("empty correlation id: %q %v", id, err)
	}
	if id, err := NewCorrelationID("trace-abc"); err != nil || id.String() != "trace-abc" {
		t.Errorf("trace-abc: %q %v", id, err)
	}
	for _, bad := range []string{"has space", "tab\tbed", strings.Repeat("x", 129)} {
		if _, err := NewCorrelationID(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestTextValues(t *testing.T) {
	cases := []struct {
		name string
		ctor func(string) (string, error)
		max  int
	}{
		{"bot name", func(s string) (string, error) { v, err := NewBotName(s); return v.String(), err }, 120},
		{"owner", func(s string) (string, error) { v, err := NewOwner(s); return v.String(), err }, 120},
		{"mission", func(s string) (string, error) { v, err := NewMission(s); return v.String(), err }, 2000},
		{"objective", func(s string) (string, error) { v, err := NewObjective(s); return v.String(), err }, 8000},
	}
	for _, c := range cases {
		if v, err := c.ctor("  Investigate 500s\nline two\ttab "); err != nil || v != "Investigate 500s\nline two\ttab" {
			t.Errorf("%s: %q %v", c.name, v, err)
		}
		if _, err := c.ctor(" "); err == nil {
			t.Errorf("%s accepted blank", c.name)
		}
		if _, err := c.ctor(strings.Repeat("x", c.max+1)); err == nil {
			t.Errorf("%s accepted too long", c.name)
		}
		if _, err := c.ctor("bell\x07"); err == nil {
			t.Errorf("%s accepted a control character", c.name)
		}
	}
}

func TestIdempotencyKey(t *testing.T) {
	if k, err := NewIdempotencyKey(" alert-2026-10-23#1 "); err != nil || k.String() != "alert-2026-10-23#1" {
		t.Errorf("%q %v", k, err)
	}
	for _, bad := range []string{"", "  ", "x\x01", strings.Repeat("k", 129)} {
		if _, err := NewIdempotencyKey(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestToolNameAndAllowedTools(t *testing.T) {
	for _, good := range []string{"observability.query_logs", "kubernetes.get-pods", "shell"} {
		if _, err := NewToolName(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "a..b", ".lead", "Upper.case", "sp ace", strings.Repeat("t", 129)} {
		if _, err := NewToolName(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	tools, err := NewAllowedTools([]string{"b.two", "a.one", "b.two"})
	if err != nil {
		t.Fatal(err)
	}
	if got := tools.Names(); len(got) != 2 || got[0] != "a.one" || got[1] != "b.two" {
		t.Errorf("Names() = %v", got)
	}
	if tools.Len() != 2 || !tools.Allows("a.one") || tools.Allows("c.three") {
		t.Error("Allows/Len wrong")
	}
	if _, err := NewAllowedTools([]string{"ok", "Not Ok"}); err == nil {
		t.Error("accepted an invalid tool in the list")
	}
	empty, _ := NewAllowedTools(nil)
	if empty.Len() != 0 || empty.Allows("x") || len(empty.Names()) != 0 {
		t.Error("empty list should allow nothing")
	}
}

func TestRoutingPolicyAndModelPolicy(t *testing.T) {
	if p, err := ParseRoutingPolicy(""); err != nil || p != RoutingCostOptimized {
		t.Errorf("default policy %q %v", p, err)
	}
	for _, good := range []string{"cost-optimized", "quality-optimized", "fixed"} {
		if p, err := ParseRoutingPolicy(good); err != nil || p.String() != good {
			t.Errorf("%q: %v", good, err)
		}
	}
	if _, err := ParseRoutingPolicy("random"); err == nil {
		t.Error("accepted random policy")
	}
	mp, err := NewModelPolicy(" anthropic/claude-haiku-5.5 ", "", false, 32000)
	if err != nil || mp.Model() != "anthropic/claude-haiku-5.5" || mp.Routing() != RoutingCostOptimized || mp.AllowRemoteProviders() || mp.MaxContextTokens() != 32000 {
		t.Errorf("model policy %+v %v", mp, err)
	}
	if _, err := NewModelPolicy("", "fixed", true, 0); err == nil {
		t.Error("fixed without a model accepted")
	}
	if _, err := NewModelPolicy("has space", "", true, 0); err == nil {
		t.Error("model with space accepted")
	}
	if _, err := NewModelPolicy(strings.Repeat("m", 201), "", true, 0); err == nil {
		t.Error("long model accepted")
	}
	if _, err := NewModelPolicy("m", "", true, -1); err == nil {
		t.Error("negative context accepted")
	}
	if _, err := NewModelPolicy("m", "weird", true, 0); err == nil {
		t.Error("bad routing accepted")
	}
}

func TestBudgets(t *testing.T) {
	b, err := NewBudgets(20, 60, 2, 900*time.Second)
	if err != nil || b.MaxModelCallsPerTask() != 20 || b.MaxToolCallsPerTask() != 60 || b.MaxDelegationDepth() != 2 || b.TaskTimeout() != 900*time.Second {
		t.Errorf("%+v %v", b, err)
	}
	if b.TaskTimeoutOr(time.Minute) != 900*time.Second {
		t.Error("TaskTimeoutOr ignored the set timeout")
	}
	zero, _ := NewBudgets(0, 0, 0, 0)
	if zero.TaskTimeoutOr(time.Minute) != time.Minute {
		t.Error("TaskTimeoutOr did not fall back")
	}
	for i, args := range [][4]int{{-1, 0, 0, 0}, {0, -1, 0, 0}, {0, 0, -1, 0}, {0, 0, 0, -1}} {
		if _, err := NewBudgets(args[0], args[1], args[2], time.Duration(args[3])); err == nil {
			t.Errorf("case %d accepted a negative budget", i)
		}
	}
}

func TestEnumParsers(t *testing.T) {
	for _, s := range []string{"Draft", "Registered", "Enabled", "Paused"} {
		if v, err := ParseBotStatus(s); err != nil || v.String() != s {
			t.Errorf("bot status %q: %v", s, err)
		}
	}
	if _, err := ParseBotStatus("Deleted"); err == nil {
		t.Error("accepted Deleted")
	}
	for _, s := range []string{"Pending", "Running", "WaitingApproval", "Completed", "Failed", "Cancelled"} {
		if v, err := ParseTaskStatus(s); err != nil || v.String() != s {
			t.Errorf("task status %q: %v", s, err)
		}
	}
	if _, err := ParseTaskStatus("Done"); err == nil {
		t.Error("accepted Done")
	}
	if !TaskCompleted.IsTerminal() || !TaskFailed.IsTerminal() || !TaskCancelled.IsTerminal() || TaskRunning.IsTerminal() || TaskPending.IsTerminal() {
		t.Error("IsTerminal wrong")
	}
	for _, s := range []string{"Running", "Completed", "Failed", "DecisionRequired"} {
		if v, err := ParseExecutionOutcome(s); err != nil || v.String() != s {
			t.Errorf("outcome %q: %v", s, err)
		}
	}
	if _, err := ParseExecutionOutcome("Meh"); err == nil {
		t.Error("accepted Meh")
	}
	if ExecutionRunning.IsFinal() || !ExecutionFailed.IsFinal() {
		t.Error("IsFinal wrong")
	}
	for _, s := range []string{"ok", "denied", "failed"} {
		if v, err := ParseAuditOutcome(s); err != nil || v.String() != s {
			t.Errorf("audit outcome %q: %v", s, err)
		}
	}
	if _, err := ParseAuditOutcome("maybe"); err == nil {
		t.Error("accepted maybe")
	}
	for _, s := range []string{"bot.registered", "bot.enabled", "bot.task.submitted", "bot.task.started", "bot.task.completed", "bot.task.failed", "bot.task.waiting_approval"} {
		if v, err := ParseEventType(s); err != nil || v.String() != s {
			t.Errorf("event type %q: %v", s, err)
		}
	}
	if _, err := ParseEventType("bot.exploded"); err == nil {
		t.Error("accepted unknown event type")
	}
	if EventSchemaV1.String() == "" {
		t.Error("empty schema version")
	}
}

func TestUsageValidate(t *testing.T) {
	if err := (Usage{Requests: 1, PromptTokens: 2, CompletionTokens: 3, ToolCalls: 4, CostUSD: 0.5}).Validate(); err != nil {
		t.Error(err)
	}
	for _, u := range []Usage{{Requests: -1}, {PromptTokens: -1}, {CompletionTokens: -1}, {ToolCalls: -1}, {CostUSD: -0.1}} {
		if err := u.Validate(); err == nil {
			t.Errorf("accepted %+v", u)
		}
	}
}

func TestErrorTypes(t *testing.T) {
	nf := NotFoundError{Kind: "bot", ID: "b1"}
	if nf.Error() != "bot b1 not found" || !IsNotFound(nf) || IsNotFound(errors.New("other")) {
		t.Error("NotFoundError wrong")
	}
	wrapped := errors.Join(errors.New("ctx"), nf)
	if !IsNotFound(wrapped) {
		t.Error("IsNotFound should see through wrapping")
	}
	if (ConflictError{Kind: "bot", ID: "b1"}).Error() != "bot b1 already exists" {
		t.Error("ConflictError wrong")
	}
	if (StateError{Subject: "task t", Current: "Completed", Attempted: "start"}).Error() != "task t is Completed: cannot start" {
		t.Error("StateError wrong")
	}
}
