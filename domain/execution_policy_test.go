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

// A phase below act-with-approval names its own tools and no host write
// tool, and a Jev bot takes no phase policy (review of #133, R4, R5).
func TestALowerPhaseLevelNeedsItsOwnReadTools(t *testing.T) {
	bot := sampleBot(t)
	tools, _ := NewAllowedTools([]string{"local_read", "local_write", "local_exec", "kubernetes.get_pods"})
	if err := bot.Redefine(bot.Mission(), tools, bot.ModelPolicy(), bot.Budgets()); err != nil {
		t.Fatal(err)
	}
	if err := bot.Govern(AutonomyActWithApproval, ToolRules{}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		trigger TriggerSnapshot
		ok      bool
	}{
		"propose without tools":     {TriggerSnapshot{Type: "incident", Name: "p", Phase: "fix", Level: "propose"}, false},
		"propose with local_write":  {TriggerSnapshot{Type: "incident", Name: "p", Phase: "fix", Level: "propose", Tools: []string{"local_read", "local_write"}}, false},
		"observe with local_exec":   {TriggerSnapshot{Type: "incident", Name: "p", Level: "observe", Tools: []string{"local_exec"}}, false},
		"propose with reads":        {TriggerSnapshot{Type: "incident", Name: "p", Phase: "fix", Level: "propose", Tools: []string{"local_read", "kubernetes.get_pods"}}, true},
		"act-with-approval, writes": {TriggerSnapshot{Type: "incident", Name: "p", Phase: "apply", Level: "act-with-approval", Tools: []string{"local_write"}}, true},
		"no level, no tools":        {TriggerSnapshot{Type: "incident", Name: "p", Phase: "apply"}, true},
	} {
		triggers, err := NewTriggers([]TriggerSnapshot{c.trigger})
		if err != nil {
			t.Fatal(err)
		}
		bot.Wire(triggers, bot.Placement())
		if err := bot.CheckTriggers(); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	jev := bot
	jev.runtime = Runtime{kind: RuntimeJev}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "incident", Name: "p", Phase: "apply", Tools: []string{"local_read"}}})
	jev.Wire(triggers, jev.Placement())
	if err := jev.CheckTriggers(); err == nil {
		t.Error("a Jev bot took a phase policy")
	}
}

func TestAPinnedPolicyIsHeldToTheBotAndKeptOnTheTask(t *testing.T) {
	bot := sampleBot(t)
	_ = bot.Govern(AutonomyActWithApproval, ToolRules{})
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "incident", Name: "apply", Phase: "apply", Tools: []string{"kubernetes.get_pods", "observability.query_logs"}}})
	bot.Wire(triggers, bot.Placement())
	task := sampleTask(t)
	_ = task.Attribute(TriggerRef{Type: TriggerIncident, Name: "apply"}, nil)
	task.Pin(bot.PolicyFor(task.Trigger()))
	back, err := RestoreTask(task.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if p, gone, _ := bot.PolicyOf(back); gone || p.Tools().Len() != 2 || p.Level() != AutonomyActWithApproval {
		t.Errorf("pinned: %v %v %s", p.Tools().Names(), gone, p.Level())
	}
	// The bot narrowed and lowered since: the pinned policy follows down, never up.
	fewer, _ := NewAllowedTools([]string{"kubernetes.get_pods"})
	_ = bot.Govern(AutonomyPropose, ToolRules{})
	_ = bot.Redefine(bot.Mission(), fewer, bot.ModelPolicy(), bot.Budgets())
	bot.Wire(DefaultTriggers(), bot.Placement())
	if p, gone, _ := bot.PolicyOf(back); !gone || p.Tools().Len() != 1 || p.Level() != AutonomyPropose {
		t.Errorf("held: %v %v %s", p.Tools().Names(), gone, p.Level())
	}
	plain := sampleTask(t)
	plain.Pin(bot.PolicyFor(ManualTrigger()))
	if _, ok := plain.Pinned(); ok || plain.Snapshot().Policy != nil {
		t.Error("a policy that narrows nothing was pinned")
	}
	broken := task.Snapshot()
	broken.Policy.Level = "root"
	if _, err := RestoreTask(broken); err == nil {
		t.Error("a pinned policy with a bad level restored")
	}
}

func TestAnIncidentAddsEachExecutionsCostOnce(t *testing.T) {
	i := sampleIncident(t)
	if added, err := i.AddExecutionCost("exec-1", Usage{CostUSD: 0.5}, t0); err != nil || !added || i.CostUSD() != 0.5 {
		t.Fatalf("first: %v %v", added, err)
	}
	if added, _ := i.AddExecutionCost("exec-1", Usage{CostUSD: 0.5}, t0); added || i.CostUSD() != 0.5 {
		t.Error("an execution counted twice")
	}
	if added, _ := i.AddExecutionCost("", Usage{CostUSD: 1}, t0); added {
		t.Error("an execution without an id counted")
	}
	if _, err := i.AddExecutionCost("exec-2", Usage{CostUSD: -1}, t0); err == nil {
		t.Error("a negative cost")
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || len(back.Snapshot().CostedExecutions) != 1 {
		t.Fatalf("round trip: %v", err)
	}
	if added, _ := back.AddExecutionCost("exec-1", Usage{CostUSD: 1}, t0); added {
		t.Error("a restored incident forgot what it counted")
	}
	s := i.Snapshot()
	s.CostedExecutions = []string{"Bad Id"}
	if _, err := RestoreIncident(s); err == nil {
		t.Error("a bad execution id restored")
	}
}

// A phase that inherits a bot level below act-with-approval may not
// inherit a host write tool no allow rule narrows; an observe bot's
// local_exec narrowed to read-only commands stays the designed exception
// (re-check of #133, N2).
func TestAnInheritedLowLevelKeepsNoUnnarrowedHostWrite(t *testing.T) {
	bot := sampleBot(t)
	tools, _ := NewAllowedTools([]string{"local_read", "local_exec", "local_write"})
	_ = bot.Redefine(bot.Mission(), tools, bot.ModelPolicy(), bot.Budgets())
	_ = bot.Govern(AutonomyObserve, ToolRules{})
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "incident"}})
	bot.Wire(triggers, bot.Placement())
	if err := bot.CheckTriggers(); err == nil {
		t.Error("an observe phase inheriting local_write and local_exec unnarrowed")
	}
	reads, _ := NewAllowedTools([]string{"local_read", "local_exec"})
	_ = bot.Govern(AutonomyObserve, ToolRules{})
	_ = bot.Redefine(bot.Mission(), reads, bot.ModelPolicy(), bot.Budgets())
	df, _ := NewToolRule("local_exec", map[string]string{"program": "df"})
	rules, _ := NewToolRules([]ToolRule{df}, nil, reads)
	_ = bot.Govern(AutonomyObserve, rules)
	if err := bot.CheckTriggers(); err != nil {
		t.Errorf("an observe triage with local_exec narrowed to df: %v", err)
	}
	task := sampleTask(t)
	_ = task.Attribute(TriggerRef{Type: TriggerIncident, Name: "incident"}, nil)
	_ = task.ForIncident("inc-1")
	if p, _, err := bot.PolicyOf(task); err != nil || !p.Tools().Allows("local_exec") {
		t.Errorf("the narrowed local_exec was dropped: %v %v", p.Tools().Names(), err)
	}
	// Without its rule the run keeps no local_exec, and with nothing left it is refused.
	_ = bot.Govern(AutonomyObserve, ToolRules{})
	if p, _, err := bot.PolicyOf(task); err != nil || p.Tools().Allows("local_exec") {
		t.Errorf("an unnarrowed local_exec at observe: %v %v", p.Tools().Names(), err)
	}
	only, _ := NewAllowedTools([]string{"local_exec"})
	_ = bot.Redefine(bot.Mission(), only, bot.ModelPolicy(), bot.Budgets())
	if _, _, err := bot.PolicyOf(task); err == nil {
		t.Error("an incident task left with no tool ran")
	}
}
