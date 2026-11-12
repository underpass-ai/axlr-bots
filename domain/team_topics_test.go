package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// docs/architecture/team-topics.md: a domain event crosses to the teams
// its emit rule names; only a plain info name, never a control signal.
func TestAnEmitRuleNamesTheTeamsADomainEventCrossesTo(t *testing.T) {
	rule, err := NewEmitRule(EmitRuleSnapshot{Name: "order.new", Teams: []string{"team-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if teams := rule.Teams(); len(teams) != 1 || teams[0] != "team-a" {
		t.Errorf("teams = %v", teams)
	}
	out, _ := json.Marshal(rule.Snapshot())
	if string(out) != `{"name":"order.new","teams":["team-a"]}` {
		t.Errorf("written: %s", out)
	}
	plain, _ := NewEmitRule(EmitRuleSnapshot{Name: "team-b.item.sorted"})
	if out, _ := json.Marshal(plain.Snapshot()); string(out) != `{"name":"team-b.item.sorted"}` {
		t.Errorf("a rule without teams: %s", out)
	}
	bad := []EmitRuleSnapshot{
		{Name: "order.*", Teams: []string{"team-a"}},
		{Name: "order.**", Teams: []string{"team-a"}},
		{Name: "stop.all", Teams: []string{"team-a"}},
		{Name: "danger.fire", Teams: []string{"team-a"}},
		{Name: "warn.stuck", Teams: []string{"team-a"}},
		{Name: "error.bot", Teams: []string{"team-a"}},
		{Name: "done.task", Teams: []string{"team-a"}},
		{Name: "order.new", Teams: []string{"Team A Team"}},
		{Name: "order.new", Teams: []string{"team-a", "team-a"}},
		{Name: "order.new", Teams: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
	}
	for _, b := range bad {
		if _, err := NewEmitRule(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestAPolicySaysWhereANameCrosses(t *testing.T) {
	p, err := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{
		{Name: "team-b.item.sorted"},
		{Name: "order.new", Teams: []string{"team-a"}},
		{Name: "order.new", Teams: []string{"producto", "team-a"}},
		{Name: "order.*"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.CrossesTo("order.new"); len(got) != 2 || got[0] != "team-a" || got[1] != "producto" {
		t.Errorf("crosses to %v", got)
	}
	for _, name := range []EventName{"team-b.item.sorted", "order.completed"} {
		if got := p.CrossesTo(name); len(got) != 0 {
			t.Errorf("%s crosses to %v", name, got)
		}
	}
	if got := (EventPolicy{}).CrossesTo("order.new"); len(got) != 0 {
		t.Errorf("the default policy crosses to %v", got)
	}
}

// A subscription that names another team hears only that team's domain
// events, as tasks; it never hears its own team's.
func TestASubscriptionHearsAnotherTeam(t *testing.T) {
	sub, err := NewSubscription(SubscriptionSnapshot{Topic: "order.new", Team: "team-b", From: []string{"b-triage"}, Deliver: "team", Objective: "team B has a new incident."})
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Crosses() || sub.Team() != "team-b" || sub.ModeFor(SeverityInfo) != ModeTask {
		t.Errorf("sub = %+v", sub)
	}
	if sub.Matches("order.new", "b-triage", PublisherBot) {
		t.Error("a crossing subscription heard its own team")
	}
	if !sub.MatchesFrom("team-b", "order.new", "b-triage") {
		t.Error("it did not hear team-b")
	}
	for _, c := range []struct {
		team TeamName
		name EventName
		from string
	}{{"team-a", "order.new", "b-triage"}, {"team-b", "order.completed", "b-triage"}, {"team-b", "order.new", "b-lead"}} {
		if sub.MatchesFrom(c.team, c.name, c.from) {
			t.Errorf("heard %+v", c)
		}
	}
	out, _ := json.Marshal(sub.Snapshot())
	if !strings.Contains(string(out), `"team":"team-b"`) {
		t.Errorf("written: %s", out)
	}
	own, _ := NewSubscription(SubscriptionSnapshot{Topic: "order.new"})
	if own.Crosses() || own.MatchesFrom("team-b", "order.new", "b-triage") || !own.Matches("order.new", "b-triage", PublisherBot) {
		t.Errorf("own = %+v", own)
	}
	if out, _ := json.Marshal(own.Snapshot()); strings.Contains(string(out), "team") {
		t.Errorf("a subscription of its own team: %s", out)
	}
	bad := []SubscriptionSnapshot{
		{Topic: "order.new", Team: "Q A"},
		{Topic: "order.new", Team: "team-b", Mode: "note"},
		{Topic: "order.new", Team: "team-b", Mode: "message"},
		{Topic: "order.new", Team: "team-b", Urgent: true},
		{Topic: "order.new", Team: "team-b", Status: "pending"},
	}
	for _, b := range bad {
		if _, err := NewSubscription(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestSubscriptionsHearAnotherTeamOnlyByASubscription(t *testing.T) {
	subs, err := NewSubscriptions([]SubscriptionSnapshot{
		{Topic: "order.new"},
		{Topic: "order.*", Team: "team-b", Mode: "task"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := subs.HearingFrom("team-b", "order.new", "b-triage")
	if !ok || sub.Team() != "team-b" {
		t.Errorf("heard by %+v %v", sub, ok)
	}
	if _, ok := subs.HearingFrom("marketing", "order.new", "mkt-1"); ok {
		t.Error("heard a team it does not name")
	}
	if _, ok := subs.HearingFrom("team-b", "stop.all", "b-triage"); ok {
		t.Error("another team's stop reached the bot without a subscription")
	}
	inside, ok := subs.Hearing("order.new", "a-go", PublisherBot, false)
	if !ok || inside.Crosses() {
		t.Errorf("inside the team: %+v %v", inside, ok)
	}
	if _, ok := subs.Hearing("order.completed", "a-go", PublisherBot, false); ok {
		t.Error("the crossing subscription heard the bot's own team")
	}
}

// A bot never crosses to, nor hears across from, one of its own teams.
func TestABotCrossesToNoneOfItsOwnTeams(t *testing.T) {
	bot := teamTestBot(t, "b-triage", "default", []string{"team-b"})
	toOwn, _ := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "order.new", Teams: []string{"team-b"}}}})
	bot.SetEvents(toOwn)
	if err := bot.CheckAsk(); err == nil {
		t.Fatal("a domain event to its own team")
	}
	toDev, _ := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "order.new", Teams: []string{"team-a"}}}})
	bot.SetEvents(toDev)
	if err := bot.CheckAsk(); err != nil {
		t.Fatal(err)
	}
	fromOwn, _ := NewSubscriptions([]SubscriptionSnapshot{{Topic: "order.completed", Team: "team-b"}})
	bot.SetSubscriptions(fromOwn)
	if err := bot.CheckAsk(); err == nil {
		t.Fatal("a subscription across from its own team")
	}
	fromDev, _ := NewSubscriptions([]SubscriptionSnapshot{{Topic: "order.completed", Team: "team-a"}})
	bot.SetSubscriptions(fromDev)
	if err := bot.CheckAsk(); err != nil {
		t.Fatal(err)
	}
}

func TestADomainEventIsOneEventPerFact(t *testing.T) {
	id, err := NewDomainID(" o-20261112-3 ")
	if err != nil || id.String() != "o-20261112-3" {
		t.Fatalf("id = %q %v", id, err)
	}
	one := id.EventIdentity("team-b", "order.new")
	if one != id.EventIdentity("team-b", "order.new") || !strings.HasPrefix(one, "evt-") || len(one) != len("evt-")+24 {
		t.Errorf("identity = %s", one)
	}
	if _, err := NewEventID(one); err != nil {
		t.Errorf("identity is no event id: %v", err)
	}
	other, _ := NewDomainID("o-20261112-4")
	for _, different := range []string{
		id.EventIdentity("team-a", "order.new"),
		id.EventIdentity("team-b", "order.completed"),
		other.EventIdentity("team-b", "order.new"),
	} {
		if different == one {
			t.Errorf("two facts share %s", one)
		}
	}
	for _, bad := range []string{"", "  ", "has space", "a/b", strings.Repeat("x", 129), "ñ"} {
		if _, err := NewDomainID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAnOriginCountsTheCrossings(t *testing.T) {
	if n, ok := NextCrossings(nil); n != 1 || !ok {
		t.Errorf("first = %d %v", n, ok)
	}
	origin := EventOrigin{EventID: "evt-1", From: "b-triage", Team: "team-b", TraceID: "trace-1", Crossings: 3}
	if err := origin.Validate(); err != nil {
		t.Fatal(err)
	}
	if n, ok := NextCrossings(&origin); n != 4 || !ok {
		t.Errorf("fourth = %d %v", n, ok)
	}
	origin.Crossings = MaxTeamCrossings
	if n, ok := NextCrossings(&origin); n != 5 || ok {
		t.Errorf("fifth = %d %v", n, ok)
	}
	for _, bad := range []EventOrigin{
		{From: "b-triage", Team: "team-b", Crossings: 1},
		{EventID: "evt-1", From: "Not A Bot", Team: "team-b", Crossings: 1},
		{EventID: "evt-1", From: "b-triage", Team: "Q A", Crossings: 1},
		{EventID: "evt-1", From: "b-triage", Team: "team-b", Crossings: 0},
		{EventID: "evt-1", From: "b-triage", Team: "team-b", Crossings: 5},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// A task made by another team's domain event starts a new chain: depth 0,
// no delegation, its origin kept through a store.
func TestATaskKeepsTheOriginOfAnotherTeamsEvent(t *testing.T) {
	task, _ := NewTask("task-1", "a-go", "Fix it", "event:evt-1:team:team-a", "trace-1", t0)
	plain, _ := json.Marshal(task.Snapshot())
	if strings.Contains(string(plain), "origin") {
		t.Errorf("a task without an origin: %s", plain)
	}
	if err := task.Cross(EventOrigin{EventID: "evt-1", From: "b-triage", Team: "team-b", Crossings: 6}); err == nil {
		t.Error("a bad origin accepted")
	}
	origin := EventOrigin{EventID: "evt-1", From: "b-triage", Team: "team-b", TraceID: "trace-0", Crossings: 1}
	if err := task.Cross(origin); err != nil {
		t.Fatal(err)
	}
	if got, ok := task.Origin(); !ok || got != origin || task.Depth() != 0 {
		t.Errorf("origin = %+v %v depth %d", got, ok, task.Depth())
	}
	lineage := TaskLineage{ParentTaskID: "task-0", ParentExecutionID: "exec-0", ParentBotID: "b-triage", Depth: 1, MaxDepth: 2, Chain: []string{"b-triage"}, Grant: DelegationGrant{Deadline: t0.Add(3600e9)}}
	if err := task.Link(lineage); err == nil {
		t.Error("a crossed task took a delegation")
	}
	restored, err := RestoreTask(task.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := restored.Origin(); !ok || got != origin {
		t.Errorf("restored origin = %+v %v", got, ok)
	}
	delegated, _ := NewTask("task-2", "a-go", "Fix it", "k-2", "trace-1", t0)
	if err := delegated.Link(lineage); err != nil {
		t.Fatal(err)
	}
	if err := delegated.Cross(origin); err == nil {
		t.Error("a delegated task took an origin")
	}
}

func TestADomainEventMayNameTheKMPKeyOfItsContext(t *testing.T) {
	key, err := NewContextKey(" project:team-b.item.o-20261112-3 ")
	if err != nil || key.String() != "project:team-b.item.o-20261112-3" {
		t.Fatalf("key = %q %v", key, err)
	}
	for _, bad := range []string{"", "project", ":x", "project:", "project:a b", "project:a\nb", "project:" + strings.Repeat("x", 250)} {
		if _, err := NewContextKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	origin := EventOrigin{EventID: "evt-1", From: "b-triage", Team: "team-b", Crossings: 1, Context: key.String()}
	if err := origin.Validate(); err != nil {
		t.Fatal(err)
	}
	origin.Context = "not a key"
	if err := origin.Validate(); err == nil {
		t.Error("an origin with a bad key")
	}
}

func TestADomainEventIsAFactWithItsIdentity(t *testing.T) {
	e, err := NewDomainEvent("order.new", "h-1", "project:team-b.item.h-1", "team-b", "b-triage", t0)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name() != "order.new" || e.ID() != "h-1" || e.Context() != "project:team-b.item.h-1" || e.Team() != "team-b" || e.From() != "b-triage" || !e.At().Equal(t0) {
		t.Errorf("event = %+v", e)
	}
	other, _ := NewDomainEvent("order.new", "h-1", "", "team-b", "b-lead", t0.Add(1))
	if other.Identity() != e.Identity() || e.Identity() != DomainID("h-1").EventIdentity("team-b", "order.new") {
		t.Errorf("two members, one fact: %s %s", e.Identity(), other.Identity())
	}
	bad := []struct {
		name EventName
		id   DomainID
		ctx  ContextKey
		team TeamName
		from BotID
	}{
		{"stop.all", "h-1", "", "team-b", "b-triage"},
		{"warn.stuck", "h-1", "", "team-b", "b-triage"},
		{"Bad Name", "h-1", "", "team-b", "b-triage"},
		{"order.new", "", "", "team-b", "b-triage"},
		{"order.new", "h-1", "no key", "team-b", "b-triage"},
		{"order.new", "h-1", "", "Q A", "b-triage"},
		{"order.new", "h-1", "", "team-b", "Not A Bot"},
	}
	if _, err := NewDomainEvent("order.new", "h-1", "", "team-b", "b-triage", time.Time{}); err == nil {
		t.Error("an event that happened at no time")
	}
	for _, b := range bad {
		if _, err := NewDomainEvent(b.name, b.id, b.ctx, b.team, b.from, t0); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestAnAudienceNamesOtherTeamsOnce(t *testing.T) {
	a, err := NewAudience("team-b", []TeamName{"team-a", "ops"})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Strings(); len(got) != 2 || got[0] != "team-a" || got[1] != "ops" || !a.Has("ops") || a.Has("team-b") || len(a.Teams()) != 2 {
		t.Errorf("audience = %v", got)
	}
	for name, raw := range map[string][]string{
		"empty":    nil,
		"repeated": {"team-a", "team-a"},
		"own":      {"team-a", "team-b"},
		"bad name": {"Team A Team"},
		"nine":     {"a", "b", "c", "d", "e", "f", "g", "h", "i"},
	} {
		if _, err := ParseAudience("team-b", raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
}

// A fact that happens again to the same thing carries the round in its id.
func TestADomainIDMayNameItsRound(t *testing.T) {
	if _, err := NewDomainID("o-20261112-3@4f9c2ab"); err != nil {
		t.Error(err)
	}
	if _, err := NewDomainID("h 1"); err == nil {
		t.Error("a space accepted")
	}
}
