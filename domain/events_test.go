package domain

import (
	"strings"
	"testing"
)

func TestEventNamesAndPatterns(t *testing.T) {
	for _, good := range []string{"halt", "deploy.started", "service-1.low-disk"} {
		if _, err := NewEventName(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	for _, bad := range []string{"", "Halt", "deploy..started", "deploy.*", ".x", strings.Repeat("a", 65), "deploy started"} {
		if _, err := NewEventName(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, err := NewEventPattern("deploy.*.**"); err != nil {
		t.Error(err)
	}
	if _, err := NewEventPattern("deploy.x*"); err == nil {
		t.Error("a partial wildcard was accepted")
	}
	cases := []struct {
		pattern, name string
		match         bool
	}{
		{"halt", "halt", true}, {"halt", "halt.now", false},
		{"deploy.*", "deploy.started", true}, {"deploy.*", "deploy", false}, {"deploy.*", "deploy.a.b", false},
		{"deploy.**", "deploy", true}, {"deploy.**", "deploy.a.b", true}, {"**", "anything.at.all", true},
		{"*.started", "deploy.started", true}, {"**.started", "a.b.started", true}, {"**.started", "a.b.lost", false},
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
	m, err := NewEventMessage("deploy 244 starting\non the service")
	if err != nil || m.String() != "deploy 244 starting on the service" {
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
	if !team.MayEmit("anything") || !team.MayEmit("stop.halt") || team.PerMinute() != DefaultEventsPerMinute || team.Snapshot() != nil {
		t.Errorf("default = %+v", team)
	}
	for _, name := range []EventName{"error.bot", "done.task"} {
		if team.MayEmit(name) {
			t.Errorf("a bot may emit %s", name)
		}
	}
	// maxSeverity is read from an older definition and ignored: the kind
	// decides.
	p, err := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "halt"}, {Name: "warn.*", MaxSeverity: "critical"}, {Name: "**", MaxSeverity: "loud"}}, PerMinute: 12})
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
	if team.IntakePerMinute() != DefaultIntakePerMinute || team.MaxDepth() != DefaultEventChainDepth {
		t.Errorf("default intake %d depth %d", team.IntakePerMinute(), team.MaxDepth())
	}
	hearing, _ := NewEventPolicy(EventsSnapshot{IntakePerMinute: 3, MaxDepth: 4})
	if hearing.IntakePerMinute() != 3 || hearing.MaxDepth() != 4 || hearing.Snapshot().IntakePerMinute != 3 || hearing.Snapshot().MaxDepth != 4 {
		t.Errorf("hearing = %+v", hearing.Snapshot())
	}
	bad := []EventsSnapshot{{PerMinute: 61}, {PerMinute: -1}, {IntakePerMinute: 61}, {MaxDepth: 9}, {MaxDepth: -1}, {Emit: []EmitRuleSnapshot{{Name: "Bad"}}}, {Emit: make([]EmitRuleSnapshot, 17)}}
	for _, b := range bad {
		if _, err := NewEventPolicy(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestEventTriggers(t *testing.T) {
	trigger, err := NewTrigger(TriggerSnapshot{Type: "event", Name: "deploys", Events: []string{"deploy.*"}})
	if err != nil {
		t.Fatal(err)
	}
	if trigger.ObjectiveTemplate() != "" {
		t.Errorf("trigger = %+v", trigger)
	}
	if s := trigger.Snapshot(); len(s.Events) != 1 || s.Events[0] != "deploy.*" {
		t.Errorf("snapshot = %+v", s)
	}
	if templated, err := NewTrigger(TriggerSnapshot{Type: "event", Events: []string{"halt"}, Objective: "Roll back: {{.message}}"}); err != nil || templated.ObjectiveTemplate() == "" || templated.Name() != "event" {
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
}

func TestBotHearsItsTeam(t *testing.T) {
	worker := teamTestBot(t, "worker-1", "default", []string{"ops"})
	if sub, ok := worker.Hears("info.x", "watcher", PublisherBot, true); !ok || !sub.Implicit() || sub.TriggerName() != "team" {
		t.Errorf("addressed info = %+v %v", sub, ok)
	}
	if _, ok := worker.Hears("warn.deploy", "watcher", PublisherBot, false); ok {
		t.Error("a warning broadcast was heard without a subscription")
	}
	if _, ok := worker.Hears("danger.fire", "watcher", PublisherBot, false); !ok {
		t.Error("a danger broadcast was not heard by default")
	}
	subs, _ := NewSubscriptions([]SubscriptionSnapshot{{Topic: "deploy.*", From: []string{"watcher"}}})
	worker.SetSubscriptions(subs)
	if sub, ok := worker.Hears("deploy.started", "watcher", PublisherBot, false); !ok || sub.TriggerName() != "deploy.*" {
		t.Errorf("subscribed = %+v %v", sub, ok)
	}
	if _, ok := worker.Hears("deploy.started", "worker-2", PublisherBot, false); ok {
		t.Error("a sender outside from was heard")
	}
	if sub, ok := worker.Hears("stop.halt", "watcher", PublisherBot, true); !ok || !sub.Implicit() {
		t.Errorf("addressed stop = %+v %v", sub, ok)
	}
	policy, _ := NewEventPolicy(EventsSnapshot{PerMinute: 3})
	worker.SetEvents(policy)
	s := worker.Snapshot()
	s.Triggers = append(s.Triggers, TriggerSnapshot{Type: "event", Events: []string{"a.*", "deploy.*"}, Interrupt: true})
	s.Model = ModelPolicySnapshot{Model: "m", RoutingPolicy: "cost-optimized"}
	s.Budgets = BudgetsSnapshot{MaxModelCallsPerTask: 1, MaxToolCallsPerTask: 1, TaskTimeoutSeconds: 60}
	s.Level, s.Status = "observe", "Registered"
	restored, err := RestoreBot(s)
	if err != nil || restored.Events().PerMinute() != 3 {
		t.Fatalf("restored = %v", err)
	}
	// A stored event trigger is read as a subscription; a pattern the bot
	// already listens to stays as it was.
	if got := restored.Snapshot(); len(got.Subscriptions) != 2 || got.Subscriptions[1].Topic != "a.*" || got.Subscriptions[1].Mode != "task" || !got.Subscriptions[1].Urgent || got.Subscriptions[0].Urgent || len(got.Triggers) != 1 {
		t.Errorf("restored subscriptions = %+v triggers %+v", got.Subscriptions, got.Triggers)
	}
	s.Events = &EventsSnapshot{PerMinute: 99}
	if _, err := RestoreBot(s); err == nil {
		t.Error("a bad rate was restored")
	}
}
