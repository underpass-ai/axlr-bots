package domain

import (
	"strings"
	"testing"
	"time"
)

func TestASubscriptionIsValidated(t *testing.T) {
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.FixedZone("x", 3600))
	sub, err := NewSubscription(SubscriptionSnapshot{Topic: " error.* ", Mode: "note", From: []string{"recepcion", "operator", "recepcion", "control-plane"}, Objective: "Retry {{.from}}", Status: "pending", AskedBy: "almacen", Why: "para reintentar", AskedAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Topic() != "error.*" || sub.Mode() != ModeNote || sub.Active() || sub.Status() != SubscriptionPending || sub.AskedBy() != "almacen" || sub.ObjectiveTemplate() != "Retry {{.from}}" || sub.Implicit() {
		t.Errorf("sub = %+v", sub)
	}
	if from := sub.From(); strings.Join(from, ",") != "control-plane,operator,recepcion" {
		t.Errorf("from = %v", from)
	}
	s := sub.Snapshot()
	if s.Status != "pending" || s.Why != "para reintentar" || s.AskedAt == nil || !s.AskedAt.Equal(at) || s.AskedAt.Location() != time.UTC {
		t.Errorf("snapshot = %+v", s)
	}
	if sub.Matches("error.bot", "recepcion", PublisherBot) {
		t.Error("a pending subscription hears")
	}
	active, _ := NewSubscription(SubscriptionSnapshot{Topic: "pedido.*"})
	if active.Snapshot().Status != "" || active.Snapshot().AskedAt != nil || !active.Active() || active.Mode() != "" {
		t.Errorf("active = %+v", active.Snapshot())
	}
	bad := []SubscriptionSnapshot{
		{Topic: "Bad"}, {Topic: "a", Mode: "loud"}, {Topic: "a", Status: "maybe"}, {Topic: "a", From: []string{"Not A Bot"}},
		{Topic: "a", From: make([]string, 17)}, {Topic: "a", Objective: "{{.broken"}, {Topic: "a", AskedBy: "Bad Id"},
		{Topic: "a", Why: strings.Repeat("x", 281)},
	}
	for _, b := range bad {
		if _, err := NewSubscription(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestModesAndSenders(t *testing.T) {
	for severity, mode := range map[Severity]DeliveryMode{SeverityInfo: ModeNote, SeverityWarning: ModeMessage, SeverityCritical: ModeTask} {
		if DefaultMode(severity) != mode || mode.String() != string(mode) {
			t.Errorf("%s → %s", severity, DefaultMode(severity))
		}
	}
	byKind, _ := NewSubscription(SubscriptionSnapshot{Topic: "**"})
	asTask, _ := NewSubscription(SubscriptionSnapshot{Topic: "**", Mode: "task"})
	if byKind.ModeFor(SeverityWarning) != ModeMessage || asTask.ModeFor(SeverityInfo) != ModeTask {
		t.Error("ModeFor is wrong")
	}
	if status, err := ParseSubscriptionStatus("active"); err != nil || status.String() != "active" {
		t.Errorf("status = %v %v", status, err)
	}
	people, _ := NewSubscription(SubscriptionSnapshot{Topic: "**", From: []string{"operator"}})
	platform, _ := NewSubscription(SubscriptionSnapshot{Topic: "**", From: []string{"control-plane"}})
	recepcion, _ := NewSubscription(SubscriptionSnapshot{Topic: "**", From: []string{"recepcion"}})
	cases := []struct {
		sub  Subscription
		from string
		by   Publisher
		want bool
	}{
		{byKind, "x", PublisherBot, true},
		{people, "", PublisherOperator, true}, {people, "recepcion", PublisherBot, false},
		{platform, "recepcion", PublisherControlPlane, true}, {platform, "recepcion", PublisherBot, false},
		{recepcion, "recepcion", PublisherBot, true}, {recepcion, "recepcion", PublisherControlPlane, true}, {recepcion, "", PublisherOperator, false},
	}
	for _, c := range cases {
		if got := c.sub.Allows(c.from, c.by); got != c.want {
			t.Errorf("%v allows %s by %s = %v", c.sub.From(), c.from, c.by, got)
		}
	}
}

func TestSubscriptionsHearByTopicThenByDefault(t *testing.T) {
	subs, err := NewSubscriptions([]SubscriptionSnapshot{{Topic: "pedido.*", From: []string{"recepcion"}}, {Topic: "**", Status: "pending"}})
	if err != nil {
		t.Fatal(err)
	}
	if sub, ok := subs.Hearing("pedido.nuevo", "recepcion", PublisherBot, false); !ok || sub.TriggerName() != "pedido.*" {
		t.Errorf("subscribed = %+v %v", sub, ok)
	}
	if _, ok := subs.Hearing("pedido.nuevo", "caja", PublisherBot, false); ok {
		t.Error("a sender outside from was heard")
	}
	if sub, ok := subs.Hearing("pedido.nuevo", "caja", PublisherBot, true); !ok || !sub.Implicit() {
		t.Errorf("addressed = %+v %v", sub, ok)
	}
	if _, ok := subs.Find("**"); !ok || len(subs.List()) != 2 || !subs.HasActive() {
		t.Error("the list is wrong")
	}
	if sub, ok := subs.Hearing("stop.all", "x", PublisherOperator, false); !ok || !sub.Implicit() {
		t.Error("a subscription to something else removed the team's default")
	}
	if _, ok := (Subscriptions{}).Hearing("info.turno", "x", PublisherBot, false); ok {
		t.Error("an info broadcast was heard without a subscription")
	}
	if (Subscriptions{}).Snapshot() != nil {
		t.Error("an empty list is not nil")
	}
	if _, err := NewSubscriptions([]SubscriptionSnapshot{{Topic: "a"}, {Topic: "a"}}); err == nil {
		t.Error("a repeated topic was accepted")
	}
	if _, err := NewSubscriptions(make([]SubscriptionSnapshot, 17)); err == nil {
		t.Error("17 subscriptions were accepted")
	}
	if _, err := NewSubscriptions([]SubscriptionSnapshot{{Topic: "Bad"}}); err == nil {
		t.Error("a bad topic was accepted")
	}
}

func TestEventTriggersAreReadAsSubscriptions(t *testing.T) {
	rest, subs := SubscriptionsFromTriggers([]TriggerSnapshot{{Type: "manual"}, {Type: "event", Events: []string{"a.*", "b"}, Interrupt: true, Objective: "Do {{.message}}"}, {Type: "event", Events: []string{"b", "c"}}}, []SubscriptionSnapshot{{Topic: "c", Mode: "note"}})
	if len(rest) != 1 || rest[0].Type != "manual" {
		t.Errorf("rest = %+v", rest)
	}
	if len(subs) != 3 || subs[0].Mode != "note" || subs[1].Topic != "a.*" || !subs[1].Urgent || subs[1].Mode != "task" || subs[1].Objective != "Do {{.message}}" || subs[2].Topic != "b" {
		t.Errorf("subs = %+v", subs)
	}
}

func TestEventTriggerRefs(t *testing.T) {
	ref, err := NewTriggerRef("event", "pedido.*")
	if err != nil || ref.Name != "pedido.*" {
		t.Errorf("ref = %+v %v", ref, err)
	}
	if old, err := NewTriggerRef("event", "ship_watch"); err != nil || old.Name != "ship_watch" {
		t.Errorf("an event trigger's name from before = %+v %v", old, err)
	}
	if _, err := NewTriggerRef("event", "Bad"); err == nil {
		t.Error("a bad event trigger name was accepted")
	}
}
