package domain

import (
	"strings"
	"testing"
)

func TestEventNamesAndPatterns(t *testing.T) {
	for _, good := range []string{"recall", "ship.sighted", "vehicle-1.low-battery"} {
		if _, err := NewEventName(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "Recall", "ship..sighted", "ship.*", ".x", strings.Repeat("a", 65), "ship sighted"} {
		if _, err := NewEventName(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, err := NewEventPattern("ship.*.**"); err != nil {
		t.Error(err)
	}
	if _, err := NewEventPattern("ship.x*"); err == nil {
		t.Error("a partial wildcard was accepted")
	}
	cases := []struct {
		pattern, name string
		match         bool
	}{
		{"recall", "recall", true}, {"recall", "recall.now", false},
		{"ship.*", "ship.sighted", true}, {"ship.*", "ship", false}, {"ship.*", "ship.a.b", false},
		{"ship.**", "ship", true}, {"ship.**", "ship.a.b", true}, {"**", "anything.at.all", true},
		{"*.sighted", "ship.sighted", true}, {"**.sighted", "a.b.sighted", true}, {"**.sighted", "a.b.lost", false},
	}
	for _, c := range cases {
		if got := EventPattern(c.pattern).Matches(EventName(c.name)); got != c.match {
			t.Errorf("%s matches %s = %v", c.pattern, c.name, got)
		}
	}
}

func TestSeverities(t *testing.T) {
	if !SeverityCritical.AtLeast(SeverityWarning) || SeverityInfo.AtLeast(SeverityWarning) || !SeverityWarning.AtLeast(SeverityWarning) {
		t.Error("order is wrong")
	}
}

func TestEventMessage(t *testing.T) {
	m, err := NewEventMessage("ship 244 entering\nthe zone")
	if err != nil || m.String() != "ship 244 entering the zone" {
		t.Errorf("message = %q %v", m, err)
	}
	if _, err := NewEventMessage(strings.Repeat("x", 281)); err == nil {
		t.Error("a long message was accepted")
	}
	if _, err := NewEventMessage("  "); err == nil {
		t.Error("an empty message was accepted")
	}
}

func TestEventPolicy(t *testing.T) {
	var team EventPolicy
	if !team.MayEmit("anything") || !team.MayEmit("stop.recall") || team.PerMinute() != DefaultEventsPerMinute || team.Snapshot() != nil {
		t.Errorf("default = %+v", team)
	}
	for _, name := range []EventName{"error.bot", "done.task"} {
		if team.MayEmit(name) {
			t.Errorf("a bot may emit %s", name)
		}
	}
	// maxSeverity is read from an older definition and ignored: the kind
	// decides.
	p, err := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "recall"}, {Name: "warn.*", MaxSeverity: "critical"}, {Name: "**", MaxSeverity: "loud"}}, PerMinute: 12})
	if err != nil {
		t.Fatal(err)
	}
	if !p.MayEmit("warn.x") || KindOf("warn.x").Severity() != SeverityWarning || p.MayEmit("error.bot") {
		t.Errorf("policy = %+v", p)
	}
	narrow, _ := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "warn.*", MaxSeverity: "critical"}}})
	if narrow.MayEmit("info.x") || !narrow.MayEmit("warn.x") {
		t.Errorf("narrow = %+v", narrow)
	}
	if p.PerMinute() != 12 || len(p.Rules()) != 3 {
		t.Errorf("policy = %+v", p)
	}
	s := p.Snapshot()
	if s == nil || len(s.Emit) != 3 || s.Emit[1].MaxSeverity != "" || s.Emit[1].Name != "warn.*" || s.PerMinute != 12 {
		t.Errorf("snapshot = %+v", s)
	}
	bad := []EventsSnapshot{{PerMinute: 61}, {PerMinute: -1}, {Emit: []EmitRuleSnapshot{{Name: "Bad"}}}, {Emit: make([]EmitRuleSnapshot, 17)}}
	for _, b := range bad {
		if _, err := NewEventPolicy(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestEventTriggers(t *testing.T) {
	trigger, err := NewTrigger(TriggerSnapshot{Type: "event", Name: "ships", Events: []string{"ship.*"}})
	if err != nil {
		t.Fatal(err)
	}
	if !trigger.Hears("ship.sighted") || trigger.Hears("recall") || trigger.ObjectiveTemplate() != "" {
		t.Errorf("trigger = %+v", trigger)
	}
	if s := trigger.Snapshot(); len(s.Events) != 1 || s.Events[0] != "ship.*" {
		t.Errorf("snapshot = %+v", s)
	}
	if templated, err := NewTrigger(TriggerSnapshot{Type: "event", Events: []string{"recall"}, Objective: "Go home: {{.message}}"}); err != nil || templated.ObjectiveTemplate() == "" || templated.Name() != "event" {
		t.Errorf("templated = %+v %v", templated, err)
	}
	bad := []TriggerSnapshot{
		{Type: "event"}, {Type: "event", Events: []string{"Bad"}}, {Type: "event", Events: []string{"x"}, Secret: strings.Repeat("s", 16)},
		{Type: "event", Events: []string{"x"}, Every: "1m"}, {Type: "manual", Events: []string{"x"}}, {Type: "event", Events: make([]string, 17)},
		{Type: "event", Events: []string{"x"}, Objective: "{{.broken"},
	}
	for _, b := range bad {
		if _, err := NewTrigger(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "manual"}, {Type: "event", Name: "ships", Events: []string{"ship.*"}}, {Type: "event", Name: "all", Events: []string{"**"}}})
	if got, ok := triggers.Hearing("ship.lost"); !ok || got.Name() != "ships" {
		t.Errorf("hearing = %+v %v", got, ok)
	}
	if got, ok := triggers.Hearing("recall"); !ok || got.Name() != "all" {
		t.Errorf("hearing recall = %+v %v", got, ok)
	}
	if _, ok := DefaultTriggers().Hearing("recall"); ok {
		t.Error("manual hears an event")
	}
}

func TestBotHearsItsTeam(t *testing.T) {
	quad := teamTestBot(t, "quad-1", "default", []string{"port"})
	if trigger, ok := quad.Hears("recall", SeverityInfo, true); !ok || trigger.Name() != "team" || trigger.Type() != TriggerEvent {
		t.Errorf("addressed info = %+v %v", trigger, ok)
	}
	if _, ok := quad.Hears("ship.sighted", SeverityInfo, false); ok {
		t.Error("an info broadcast was heard by default")
	}
	if _, ok := quad.Hears("ship.sighted", SeverityWarning, false); !ok {
		t.Error("a warning broadcast was not heard by default")
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "event", Name: "ships", Events: []string{"ship.*"}}})
	quad.Wire(triggers, Placement{})
	if _, ok := quad.Hears("recall", SeverityCritical, true); ok {
		t.Error("declared triggers still take the default")
	}
	if trigger, ok := quad.Hears("ship.sighted", SeverityInfo, false); !ok || trigger.Name() != "ships" {
		t.Errorf("declared = %+v %v", trigger, ok)
	}
	policy, _ := NewEventPolicy(EventsSnapshot{PerMinute: 3})
	quad.SetEvents(policy)
	s := quad.Snapshot()
	s.Model = ModelPolicySnapshot{Model: "m", RoutingPolicy: "cost-optimized"}
	s.Budgets = BudgetsSnapshot{MaxModelCallsPerTask: 1, MaxToolCallsPerTask: 1, TaskTimeoutSeconds: 60}
	s.Level, s.Status = "observe", "Registered"
	restored, err := RestoreBot(s)
	if err != nil || restored.Events().PerMinute() != 3 {
		t.Fatalf("restored = %v", err)
	}
	s.Events = &EventsSnapshot{PerMinute: 99}
	if _, err := RestoreBot(s); err == nil {
		t.Error("a bad rate was restored")
	}
}
