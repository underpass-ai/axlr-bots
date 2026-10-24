package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestAutonomyLevel(t *testing.T) {
	for raw, want := range map[string]AutonomyLevel{"": AutonomyObserve, "observe": AutonomyObserve, "propose": AutonomyPropose, "act-with-approval": AutonomyActWithApproval, "act-within-policy": AutonomyActWithinPolicy} {
		got, err := ParseAutonomyLevel(raw)
		if err != nil || got != want {
			t.Errorf("%q: %v %s", raw, err, got)
		}
	}
	var verr ValidationError
	if _, err := ParseAutonomyLevel("yolo"); !errors.As(err, &verr) {
		t.Errorf("bad level: %v", err)
	}
	if AutonomyObserve.Writes() || AutonomyPropose.Writes() || !AutonomyActWithApproval.Writes() || !AutonomyActWithinPolicy.Writes() {
		t.Error("Writes wrong")
	}
	if AutonomyPropose.String() != "propose" {
		t.Error("String wrong")
	}
}

func TestToolRule(t *testing.T) {
	rule, err := NewToolRule("local_exec", map[string]string{"program": "journalctl", "args": `["-u",**`})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Tool() != "local_exec" || len(rule.Arguments()) != 2 || rule.Arguments()["program"] != "journalctl" {
		t.Errorf("rule = %+v", rule.Snapshot())
	}
	if names := rule.ArgumentNames(); len(names) != 2 || names[0] != "args" || names[1] != "program" {
		t.Errorf("names = %v", names)
	}
	rule.Arguments()["program"] = "rm" // a copy
	if rule.Arguments()["program"] != "journalctl" {
		t.Error("Arguments is not a copy")
	}
	whole, _ := NewToolRule("local_read", nil)
	if whole.Arguments() != nil || len(whole.ArgumentNames()) != 0 || whole.Snapshot().Arguments != nil {
		t.Error("a rule without patterns has arguments")
	}
	long := make([]byte, 257)
	for i := range long {
		long[i] = 'a'
	}
	bad := []struct {
		tool string
		args map[string]string
	}{
		{"", nil},
		{"Local_Exec", nil},
		{"local_exec", map[string]string{"": "x"}},
		{"local_exec", map[string]string{"program": ""}},
		{"local_exec", map[string]string{"program": "a\tb"}},
		{"local_exec", map[string]string{"pro\ngram": "x"}},
		{"local_exec", map[string]string{"program": string(long)}},
		{"local_exec", map[string]string{string(long[:65]): "x"}},
		{"local_exec", map[string]string{"program": "\xff"}},
	}
	for i, b := range bad {
		if _, err := NewToolRule(b.tool, b.args); err == nil {
			t.Errorf("bad rule %d accepted", i)
		}
	}
}

func TestToolRules(t *testing.T) {
	allowed, _ := NewAllowedTools([]string{"local_exec", "local_read"})
	exec, _ := NewToolRule("local_exec", map[string]string{"program": "df"})
	deny, _ := NewToolRule("local_write", nil)
	rules, err := NewToolRules([]ToolRule{exec}, []ToolRule{deny}, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if rules.IsEmpty() || !rules.Constrains("local_exec") || rules.Constrains("local_read") || len(rules.Allow()) != 1 || len(rules.Deny()) != 1 {
		t.Errorf("rules = %+v", rules.Snapshot())
	}
	if (ToolRules{}).IsEmpty() != true {
		t.Error("zero rules are not empty")
	}
	other, _ := NewToolRule("local_write", nil)
	if _, err := NewToolRules([]ToolRule{other}, nil, allowed); err == nil {
		t.Error("allow rule outside the allow-list accepted")
	}
	snap := rules.Snapshot()
	raw, _ := json.Marshal(snap)
	var back ToolRulesSnapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreToolRules(back, allowed)
	if err != nil || len(restored.Allow()) != 1 || restored.Allow()[0].Arguments()["program"] != "df" || restored.Deny()[0].Tool() != "local_write" {
		t.Errorf("restored = %v %+v", err, restored.Snapshot())
	}
	if _, err := RestoreToolRules(ToolRulesSnapshot{Allow: []ToolRuleSnapshot{{Tool: ""}}}, allowed); err == nil {
		t.Error("bad allow snapshot restored")
	}
	if _, err := RestoreToolRules(ToolRulesSnapshot{Deny: []ToolRuleSnapshot{{Tool: "Bad"}}}, allowed); err == nil {
		t.Error("bad deny snapshot restored")
	}
}

func TestBotGovernance(t *testing.T) {
	bot := sampleBot(t)
	if bot.Level() != AutonomyObserve || !bot.ToolRules().IsEmpty() {
		t.Fatal("a new bot is not observe without rules")
	}
	rule, _ := NewToolRule("observability.query_logs", map[string]string{"service": "api-*"})
	rules, _ := NewToolRules([]ToolRule{rule}, nil, bot.AllowedTools())
	if err := bot.Govern(AutonomyActWithApproval, rules); err != nil {
		t.Fatal(err)
	}
	if bot.Level() != AutonomyActWithApproval || !bot.ToolRules().Constrains("observability.query_logs") {
		t.Error("govern did not apply")
	}
	if err := bot.Govern("yolo", rules); err == nil {
		t.Error("bad level accepted")
	}
	foreign, _ := NewToolRule("local_exec", nil)
	if err := bot.Govern(AutonomyObserve, ToolRules{allow: []ToolRule{foreign}}); err == nil {
		t.Error("rule outside the allow-list accepted")
	}
	_ = bot.Register(t0)
	raw, _ := json.Marshal(bot.Snapshot())
	var snap BotSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Level != "act-with-approval" || len(snap.ToolRules.Allow) != 1 {
		t.Errorf("snapshot = %+v", snap)
	}
	restored, err := RestoreBot(snap)
	if err != nil || restored.Level() != AutonomyActWithApproval || restored.ToolRules().Allow()[0].Arguments()["service"] != "api-*" {
		t.Errorf("restored = %v %+v", err, restored.Snapshot())
	}
	snap.Level = ""
	if restored, err := RestoreBot(snap); err != nil || restored.Level() != AutonomyObserve {
		t.Errorf("missing level: %v %s", err, restored.Level())
	}
	snap.Level = "yolo"
	if _, err := RestoreBot(snap); err == nil {
		t.Error("bad level restored")
	}
	snap.Level = "observe"
	snap.ToolRules.Allow[0].Tool = "kubernetes.get_events"
	if _, err := RestoreBot(snap); err == nil {
		t.Error("rule outside the allow-list restored")
	}
}

func TestExecutionPolicyRecord(t *testing.T) {
	e := sampleExecution(t)
	if e.Policy() != (PolicyRecord{}) || e.Snapshot().Policy != nil {
		t.Fatal("a new execution has a policy")
	}
	if err := e.RecordPolicy(PolicyRecord{}); err == nil {
		t.Error("empty level recorded")
	}
	record := PolicyRecord{Level: "observe", RuntimeLevel: "act-within-policy", Enforced: true, Path: "/state/policies/exec-1.json"}
	if err := e.RecordPolicy(record); err != nil {
		t.Fatal(err)
	}
	_ = e.Complete(Result{Answer: "a"}, Usage{}, "/t", t0.Add(time.Second))
	raw, _ := json.Marshal(e.Snapshot())
	var snap ExecutionSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	back, err := RestoreExecution(snap)
	if err != nil || back.Policy() != record {
		t.Errorf("restored policy = %v %+v", err, back.Policy())
	}
	snap.Policy = &PolicyRecord{}
	if _, err := RestoreExecution(snap); err == nil {
		t.Error("empty policy restored")
	}
}
