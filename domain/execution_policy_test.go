package domain

import (
	"slices"
	"testing"
)

func TestIncidentTriggerNarrowsThePhasePolicy(t *testing.T) {
	bot := sampleBot(t)
	if err := bot.Govern(AutonomyActWithApproval, ToolRules{}); err != nil {
		t.Fatal(err)
	}
	triggers, err := NewTriggers([]TriggerSnapshot{
		{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"kubernetes.get_pods"}},
		{Type: "incident", Name: "apply", Phase: "apply"},
		{Type: "manual"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bot.Wire(triggers, bot.Placement())
	if err := bot.CheckTriggers(); err != nil {
		t.Fatalf("a narrower phase refused: %v", err)
	}
	fix := bot.PolicyFor(TriggerRef{Type: TriggerIncident, Name: "fix"})
	if fix.Level() != AutonomyPropose || !slices.Equal(fix.Tools().Names(), []string{"kubernetes.get_pods"}) || !fix.Narrowed() {
		t.Errorf("fix phase: %s %v %v", fix.Level(), fix.Tools().Names(), fix.Narrowed())
	}
	for _, ref := range []TriggerRef{{Type: TriggerIncident, Name: "apply"}, ManualTrigger(), {Type: TriggerIncident, Name: "gone"}, {Type: TriggerManual, Name: "fix"}} {
		if p := bot.PolicyFor(ref); p.Level() != AutonomyActWithApproval || p.Tools().Len() != 2 || p.Narrowed() {
			t.Errorf("%+v: %s %v", ref, p.Level(), p.Tools().Names())
		}
	}
	stored, err := NewTrigger(triggers.List()[0].Snapshot())
	if err != nil || stored.Snapshot().Level != "propose" || len(stored.Snapshot().Tools) != 1 {
		t.Errorf("round trip: %+v %v", stored.Snapshot(), err)
	}
	if s := triggers.List()[1].Snapshot(); s.Level != "" || s.Tools != nil {
		t.Errorf("a trigger that narrows nothing stores %+v", s)
	}

	for name, wide := range map[string]TriggerSnapshot{
		"a higher level":   {Type: "incident", Name: "apply", Phase: "apply", Level: "act-within-policy"},
		"a tool not given": {Type: "incident", Name: "apply", Phase: "apply", Tools: []string{"kubernetes.apply"}},
	} {
		widened, err := NewTriggers([]TriggerSnapshot{wide})
		if err != nil {
			t.Fatal(err)
		}
		bot.Wire(widened, bot.Placement())
		if err := bot.CheckTriggers(); err == nil {
			t.Errorf("%s: accepted", name)
		}
		// Even unchecked, a trigger never raises the level.
		if p := bot.PolicyFor(TriggerRef{Type: TriggerIncident, Name: "apply"}); p.Level() != AutonomyActWithApproval {
			t.Errorf("%s: level %s", name, p.Level())
		}
	}
	for _, bad := range []TriggerSnapshot{
		{Type: "manual", Level: "observe"},
		{Type: "webhook", Secret: "0123456789abcdef", Tools: []string{"x"}},
		{Type: "incident", Level: "root"},
		{Type: "incident", Tools: []string{"a..b"}},
	} {
		if _, err := NewTrigger(bad); err == nil {
			t.Errorf("%+v validated", bad)
		}
	}
}

func TestAutonomyLevelOrder(t *testing.T) {
	order := []AutonomyLevel{AutonomyObserve, AutonomyPropose, AutonomyActWithApproval, AutonomyActWithinPolicy}
	for n, low := range order {
		for m, high := range order {
			if low.AtMost(high) != (n <= m) {
				t.Errorf("%s at most %s = %v", low, high, low.AtMost(high))
			}
		}
	}
}

func TestANarrowerListDropsTheAllowRulesOfToolsItLeavesOut(t *testing.T) {
	bot := sampleBot(t)
	logs, _ := NewToolRule("observability.query_logs", map[string]string{"service": "web"})
	pods, _ := NewToolRule("kubernetes.get_pods", map[string]string{"namespace": "shop"})
	never, _ := NewToolRule("kubernetes.delete", nil)
	rules, err := NewToolRules([]ToolRule{logs, pods}, []ToolRule{never}, bot.AllowedTools())
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Govern(AutonomyPropose, rules); err != nil {
		t.Fatal(err)
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "incident", Name: "triage", Tools: []string{"kubernetes.get_pods"}}})
	bot.Wire(triggers, bot.Placement())
	narrowed := bot.PolicyFor(TriggerRef{Type: TriggerIncident, Name: "triage"}).Rules()
	if allow := narrowed.Allow(); len(allow) != 1 || allow[0].Tool() != "kubernetes.get_pods" || len(narrowed.Deny()) != 1 {
		t.Errorf("rules = %+v", narrowed.Snapshot())
	}
	if whole := bot.PolicyFor(ManualTrigger()).Rules(); len(whole.Allow()) != 2 {
		t.Errorf("the bot's rules = %+v", whole.Snapshot())
	}
}
