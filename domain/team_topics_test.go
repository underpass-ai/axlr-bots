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
	rule, err := NewEmitRule(EmitRuleSnapshot{Name: "incidencia.nueva", Teams: []string{"dev"}})
	if err != nil {
		t.Fatal(err)
	}
	if teams := rule.Teams(); len(teams) != 1 || teams[0] != "dev" {
		t.Errorf("teams = %v", teams)
	}
	out, _ := json.Marshal(rule.Snapshot())
	if string(out) != `{"name":"incidencia.nueva","teams":["dev"]}` {
		t.Errorf("written: %s", out)
	}
	plain, _ := NewEmitRule(EmitRuleSnapshot{Name: "qa.hallazgo.triado"})
	if out, _ := json.Marshal(plain.Snapshot()); string(out) != `{"name":"qa.hallazgo.triado"}` {
		t.Errorf("a rule without teams: %s", out)
	}
	bad := []EmitRuleSnapshot{
		{Name: "incidencia.*", Teams: []string{"dev"}},
		{Name: "incidencia.**", Teams: []string{"dev"}},
		{Name: "stop.all", Teams: []string{"dev"}},
		{Name: "danger.fire", Teams: []string{"dev"}},
		{Name: "warn.stuck", Teams: []string{"dev"}},
		{Name: "error.bot", Teams: []string{"dev"}},
		{Name: "done.task", Teams: []string{"dev"}},
		{Name: "incidencia.nueva", Teams: []string{"Dev Team"}},
		{Name: "incidencia.nueva", Teams: []string{"dev", "dev"}},
		{Name: "incidencia.nueva", Teams: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
	}
	for _, b := range bad {
		if _, err := NewEmitRule(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestAPolicySaysWhereANameCrosses(t *testing.T) {
	p, err := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{
		{Name: "qa.hallazgo.triado"},
		{Name: "incidencia.nueva", Teams: []string{"dev"}},
		{Name: "incidencia.nueva", Teams: []string{"producto", "dev"}},
		{Name: "incidencia.*"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.CrossesTo("incidencia.nueva"); len(got) != 2 || got[0] != "dev" || got[1] != "producto" {
		t.Errorf("crosses to %v", got)
	}
	for _, name := range []EventName{"qa.hallazgo.triado", "incidencia.completada"} {
		if got := p.CrossesTo(name); len(got) != 0 {
			t.Errorf("%s crosses to %v", name, got)
		}
	}
	if got := (EventPolicy{}).CrossesTo("incidencia.nueva"); len(got) != 0 {
		t.Errorf("the default policy crosses to %v", got)
	}
}

// A subscription that names another team hears only that team's domain
// events, as tasks; it never hears its own team's.
func TestASubscriptionHearsAnotherTeam(t *testing.T) {
	sub, err := NewSubscription(SubscriptionSnapshot{Topic: "incidencia.nueva", Team: "qa", From: []string{"qa-triaje"}, Deliver: "team", Objective: "QA has a new incident."})
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Crosses() || sub.Team() != "qa" || sub.ModeFor(SeverityInfo) != ModeTask {
		t.Errorf("sub = %+v", sub)
	}
	if sub.Matches("incidencia.nueva", "qa-triaje", PublisherBot) {
		t.Error("a crossing subscription heard its own team")
	}
	if !sub.MatchesFrom("qa", "incidencia.nueva", "qa-triaje") {
		t.Error("it did not hear qa")
	}
	for _, c := range []struct {
		team TeamName
		name EventName
		from string
	}{{"dev", "incidencia.nueva", "qa-triaje"}, {"qa", "incidencia.completada", "qa-triaje"}, {"qa", "incidencia.nueva", "qa-jefe"}} {
		if sub.MatchesFrom(c.team, c.name, c.from) {
			t.Errorf("heard %+v", c)
		}
	}
	out, _ := json.Marshal(sub.Snapshot())
	if !strings.Contains(string(out), `"team":"qa"`) {
		t.Errorf("written: %s", out)
	}
	own, _ := NewSubscription(SubscriptionSnapshot{Topic: "incidencia.nueva"})
	if own.Crosses() || own.MatchesFrom("qa", "incidencia.nueva", "qa-triaje") || !own.Matches("incidencia.nueva", "qa-triaje", PublisherBot) {
		t.Errorf("own = %+v", own)
	}
	if out, _ := json.Marshal(own.Snapshot()); strings.Contains(string(out), "team") {
		t.Errorf("a subscription of its own team: %s", out)
	}
	bad := []SubscriptionSnapshot{
		{Topic: "incidencia.nueva", Team: "Q A"},
		{Topic: "incidencia.nueva", Team: "qa", Mode: "note"},
		{Topic: "incidencia.nueva", Team: "qa", Mode: "message"},
		{Topic: "incidencia.nueva", Team: "qa", Urgent: true},
		{Topic: "incidencia.nueva", Team: "qa", Status: "pending"},
	}
	for _, b := range bad {
		if _, err := NewSubscription(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestSubscriptionsHearAnotherTeamOnlyByASubscription(t *testing.T) {
	subs, err := NewSubscriptions([]SubscriptionSnapshot{
		{Topic: "incidencia.nueva"},
		{Topic: "incidencia.*", Team: "qa", Mode: "task"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, ok := subs.HearingFrom("qa", "incidencia.nueva", "qa-triaje")
	if !ok || sub.Team() != "qa" {
		t.Errorf("heard by %+v %v", sub, ok)
	}
	if _, ok := subs.HearingFrom("marketing", "incidencia.nueva", "mkt-1"); ok {
		t.Error("heard a team it does not name")
	}
	if _, ok := subs.HearingFrom("qa", "stop.all", "qa-triaje"); ok {
		t.Error("another team's stop reached the bot without a subscription")
	}
	inside, ok := subs.Hearing("incidencia.nueva", "dev-go", PublisherBot, false)
	if !ok || inside.Crosses() {
		t.Errorf("inside the team: %+v %v", inside, ok)
	}
	if _, ok := subs.Hearing("incidencia.completada", "dev-go", PublisherBot, false); ok {
		t.Error("the crossing subscription heard the bot's own team")
	}
}

// A bot never crosses to, nor hears across from, one of its own teams.
func TestABotCrossesToNoneOfItsOwnTeams(t *testing.T) {
	bot := teamTestBot(t, "qa-triaje", "default", []string{"qa"})
	toOwn, _ := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "incidencia.nueva", Teams: []string{"qa"}}}})
	bot.SetEvents(toOwn)
	if err := bot.CheckAsk(); err == nil {
		t.Fatal("a domain event to its own team")
	}
	toDev, _ := NewEventPolicy(EventsSnapshot{Emit: []EmitRuleSnapshot{{Name: "incidencia.nueva", Teams: []string{"dev"}}}})
	bot.SetEvents(toDev)
	if err := bot.CheckAsk(); err != nil {
		t.Fatal(err)
	}
	fromOwn, _ := NewSubscriptions([]SubscriptionSnapshot{{Topic: "incidencia.completada", Team: "qa"}})
	bot.SetSubscriptions(fromOwn)
	if err := bot.CheckAsk(); err == nil {
		t.Fatal("a subscription across from its own team")
	}
	fromDev, _ := NewSubscriptions([]SubscriptionSnapshot{{Topic: "incidencia.completada", Team: "dev"}})
	bot.SetSubscriptions(fromDev)
	if err := bot.CheckAsk(); err != nil {
		t.Fatal(err)
	}
}

func TestADomainEventIsOneEventPerFact(t *testing.T) {
	id, err := NewDomainID(" h-20261112-kmp-3 ")
	if err != nil || id.String() != "h-20261112-kmp-3" {
		t.Fatalf("id = %q %v", id, err)
	}
	one := id.EventIdentity("qa", "incidencia.nueva")
	if one != id.EventIdentity("qa", "incidencia.nueva") || !strings.HasPrefix(one, "evt-") || len(one) != len("evt-")+24 {
		t.Errorf("identity = %s", one)
	}
	if _, err := NewEventID(one); err != nil {
		t.Errorf("identity is no event id: %v", err)
	}
	other, _ := NewDomainID("h-20261112-kmp-4")
	for _, different := range []string{
		id.EventIdentity("dev", "incidencia.nueva"),
		id.EventIdentity("qa", "incidencia.completada"),
		other.EventIdentity("qa", "incidencia.nueva"),
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
	origin := EventOrigin{EventID: "evt-1", From: "qa-triaje", Team: "qa", TraceID: "trace-1", Crossings: 3}
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
		{From: "qa-triaje", Team: "qa", Crossings: 1},
		{EventID: "evt-1", From: "Not A Bot", Team: "qa", Crossings: 1},
		{EventID: "evt-1", From: "qa-triaje", Team: "Q A", Crossings: 1},
		{EventID: "evt-1", From: "qa-triaje", Team: "qa", Crossings: 0},
		{EventID: "evt-1", From: "qa-triaje", Team: "qa", Crossings: 5},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// A task made by another team's domain event starts a new chain: depth 0,
// no delegation, its origin kept through a store.
func TestATaskKeepsTheOriginOfAnotherTeamsEvent(t *testing.T) {
	task, _ := NewTask("task-1", "dev-go", "Fix it", "event:evt-1:team:dev", "trace-1", t0)
	plain, _ := json.Marshal(task.Snapshot())
	if strings.Contains(string(plain), "origin") {
		t.Errorf("a task without an origin: %s", plain)
	}
	if err := task.Cross(EventOrigin{EventID: "evt-1", From: "qa-triaje", Team: "qa", Crossings: 6}); err == nil {
		t.Error("a bad origin accepted")
	}
	origin := EventOrigin{EventID: "evt-1", From: "qa-triaje", Team: "qa", TraceID: "trace-0", Crossings: 1}
	if err := task.Cross(origin); err != nil {
		t.Fatal(err)
	}
	if got, ok := task.Origin(); !ok || got != origin || task.Depth() != 0 {
		t.Errorf("origin = %+v %v depth %d", got, ok, task.Depth())
	}
	lineage := TaskLineage{ParentTaskID: "task-0", ParentExecutionID: "exec-0", ParentBotID: "qa-triaje", Depth: 1, MaxDepth: 2, Chain: []string{"qa-triaje"}, Grant: DelegationGrant{Deadline: t0.Add(3600e9)}}
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
	delegated, _ := NewTask("task-2", "dev-go", "Fix it", "k-2", "trace-1", t0)
	if err := delegated.Link(lineage); err != nil {
		t.Fatal(err)
	}
	if err := delegated.Cross(origin); err == nil {
		t.Error("a delegated task took an origin")
	}
}

func TestADomainEventMayNameTheKMPKeyOfItsContext(t *testing.T) {
	key, err := NewContextKey(" project:qa.hallazgo.h-20261112-kmp-3 ")
	if err != nil || key.String() != "project:qa.hallazgo.h-20261112-kmp-3" {
		t.Fatalf("key = %q %v", key, err)
	}
	for _, bad := range []string{"", "project", ":x", "project:", "project:a b", "project:a\nb", "project:" + strings.Repeat("x", 250)} {
		if _, err := NewContextKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	origin := EventOrigin{EventID: "evt-1", From: "qa-triaje", Team: "qa", Crossings: 1, Context: key.String()}
	if err := origin.Validate(); err != nil {
		t.Fatal(err)
	}
	origin.Context = "not a key"
	if err := origin.Validate(); err == nil {
		t.Error("an origin with a bad key")
	}
}

func TestADomainEventIsAFactWithItsIdentity(t *testing.T) {
	e, err := NewDomainEvent("incidencia.nueva", "h-1", "project:qa.hallazgo.h-1", "qa", "qa-triaje", t0)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name() != "incidencia.nueva" || e.ID() != "h-1" || e.Context() != "project:qa.hallazgo.h-1" || e.Team() != "qa" || e.From() != "qa-triaje" || !e.At().Equal(t0) {
		t.Errorf("event = %+v", e)
	}
	other, _ := NewDomainEvent("incidencia.nueva", "h-1", "", "qa", "qa-jefe", t0.Add(1))
	if other.Identity() != e.Identity() || e.Identity() != DomainID("h-1").EventIdentity("qa", "incidencia.nueva") {
		t.Errorf("two members, one fact: %s %s", e.Identity(), other.Identity())
	}
	bad := []struct {
		name EventName
		id   DomainID
		ctx  ContextKey
		team TeamName
		from BotID
	}{
		{"stop.all", "h-1", "", "qa", "qa-triaje"},
		{"warn.stuck", "h-1", "", "qa", "qa-triaje"},
		{"Bad Name", "h-1", "", "qa", "qa-triaje"},
		{"incidencia.nueva", "", "", "qa", "qa-triaje"},
		{"incidencia.nueva", "h-1", "no key", "qa", "qa-triaje"},
		{"incidencia.nueva", "h-1", "", "Q A", "qa-triaje"},
		{"incidencia.nueva", "h-1", "", "qa", "Not A Bot"},
	}
	if _, err := NewDomainEvent("incidencia.nueva", "h-1", "", "qa", "qa-triaje", time.Time{}); err == nil {
		t.Error("an event that happened at no time")
	}
	for _, b := range bad {
		if _, err := NewDomainEvent(b.name, b.id, b.ctx, b.team, b.from, t0); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestAnAudienceNamesOtherTeamsOnce(t *testing.T) {
	a, err := NewAudience("qa", []TeamName{"dev", "ops"})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Strings(); len(got) != 2 || got[0] != "dev" || got[1] != "ops" || !a.Has("ops") || a.Has("qa") || len(a.Teams()) != 2 {
		t.Errorf("audience = %v", got)
	}
	for name, raw := range map[string][]string{
		"empty":    nil,
		"repeated": {"dev", "dev"},
		"own":      {"dev", "qa"},
		"bad name": {"Dev Team"},
		"nine":     {"a", "b", "c", "d", "e", "f", "g", "h", "i"},
	} {
		if _, err := ParseAudience("qa", raw); err == nil {
			t.Errorf("%s: accepted %v", name, raw)
		}
	}
}

// A fact that happens again to the same thing carries the round in its id.
func TestADomainIDMayNameItsRound(t *testing.T) {
	if _, err := NewDomainID("h-20261112-kmp-3@4f9c2ab"); err != nil {
		t.Error(err)
	}
	if _, err := NewDomainID("h 1"); err == nil {
		t.Error("a space accepted")
	}
}
