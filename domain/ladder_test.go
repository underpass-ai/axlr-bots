package domain

import (
	"strings"
	"testing"
)

func ladderSnapshot() *LadderSnapshot {
	return &LadderSnapshot{Runbooks: []LadderRunbookSnapshot{{Name: "Drift", Tool: "patch", Namespaces: []string{"shop"}}}}
}

func ladderTrigger() TriggerSnapshot {
	return TriggerSnapshot{Type: "incident", Name: "apply", Phase: "apply", Level: "act-with-approval", Tools: []string{"local_read", "patch", "scale"}, Ladder: ladderSnapshot()}
}

func TestNewLadderValidates(t *testing.T) {
	for name, s := range map[string]LadderSnapshot{
		"no runbooks":   {},
		"bad name":      {Runbooks: []LadderRunbookSnapshot{{Name: "a b", Tool: "patch", Namespaces: []string{"shop"}}}},
		"bad tool":      {Runbooks: []LadderRunbookSnapshot{{Name: "A", Tool: "", Namespaces: []string{"shop"}}}},
		"no namespaces": {Runbooks: []LadderRunbookSnapshot{{Name: "A", Tool: "patch"}}},
		"bad namespace": {Runbooks: []LadderRunbookSnapshot{{Name: "A", Tool: "patch", Namespaces: []string{"Bad"}}}},
		"twice ns":      {Runbooks: []LadderRunbookSnapshot{{Name: "A", Tool: "patch", Namespaces: []string{"a", "a"}}}},
		"twice name":    {Runbooks: []LadderRunbookSnapshot{{Name: "A", Tool: "patch", Namespaces: []string{"a"}}, {Name: "A", Tool: "patch", Namespaces: []string{"b"}}}},
		"thresholds":    {Runbooks: []LadderRunbookSnapshot{{Name: "A", Tool: "patch", Namespaces: []string{"a"}}}, Thresholds: &AutonomyThresholdsSnapshot{Applies: -1}},
	} {
		if _, err := NewLadder(s); err == nil {
			t.Errorf("%s passed", name)
		}
	}
	ladder, err := NewLadder(*ladderSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ladder.Runbook("Other"); ok {
		t.Error("found an unknown runbook")
	}
	if len(ladder.Runbooks()) != 1 || ladder.Snapshot().Thresholds.Applies != 20 {
		t.Errorf("snapshot %+v", ladder.Snapshot())
	}
}

func TestOnlyTheApplyTriggerTakesALadder(t *testing.T) {
	if _, err := NewTrigger(TriggerSnapshot{Type: "manual", Ladder: ladderSnapshot()}); err == nil {
		t.Error("a manual trigger took a ladder")
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "incident", Phase: "fix", Ladder: ladderSnapshot()}); err == nil {
		t.Error("a fix trigger took a ladder")
	}
	bad := ladderTrigger()
	bad.Ladder = &LadderSnapshot{}
	if _, err := NewTrigger(bad); err == nil {
		t.Error("an empty ladder passed")
	}
	trigger, err := NewTrigger(ladderTrigger())
	if err != nil {
		t.Fatal(err)
	}
	if ladder, ok := trigger.Ladder(); !ok || len(ladder.Runbooks()) != 1 {
		t.Fatalf("ladder %+v %v", ladder, ok)
	}
	if trigger.Snapshot().Ladder == nil {
		t.Fatal("the snapshot lost the ladder")
	}
	plain, _ := NewTrigger(TriggerSnapshot{Type: "incident", Phase: "apply"})
	if _, ok := plain.Ladder(); ok {
		t.Fatal("no ladder was asked for")
	}
}

func checkOf(t *testing.T, level string, trigger TriggerSnapshot) error {
	t.Helper()
	bot := phaseBot(t, level, []string{"local_read", "local_write", "patch", "scale"}, []TriggerSnapshot{trigger})
	return bot.CheckTriggers()
}

func TestCheckTriggersRefusesALadderThatCannotEarnSafely(t *testing.T) {
	good := ladderTrigger()
	if err := checkOf(t, "act-within-policy", good); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		level  string
		mutate func(*TriggerSnapshot)
		want   string
	}{
		"bot below the top":  {"act-with-approval", func(*TriggerSnapshot) {}, "at act-within-policy"},
		"no level":           {"act-within-policy", func(s *TriggerSnapshot) { s.Level = "" }, "act-with-approval"},
		"started at the top": {"act-within-policy", func(s *TriggerSnapshot) { s.Level = "act-within-policy" }, "act-with-approval"},
		"no tools":           {"act-within-policy", func(s *TriggerSnapshot) { s.Tools = nil }, "the phase's tools"},
		"tool not listed":    {"act-within-policy", func(s *TriggerSnapshot) { s.Tools = []string{"local_read"} }, "is not in the phase's tools"},
		"host write":         {"act-within-policy", func(s *TriggerSnapshot) { s.Tools = []string{"local_write"}; s.Ladder.Runbooks[0].Tool = "local_write" }, "the runtime's own"},
	} {
		trigger := ladderTrigger()
		c.mutate(&trigger)
		if err := checkOf(t, c.level, trigger); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func topAutonomy(t *testing.T) RunbookAutonomy {
	t.Helper()
	a := newTestAutonomy(t, AutonomyActWithApproval)
	for n := 1; n <= 20; n++ {
		record(t, &a, SignalDiagnosisCorrect, "inc-a", n, "tirso")
		record(t, &a, SignalProposalApproved, "inc-a", n, "tirso")
		record(t, &a, SignalApplyVerified, "inc-a", n, "")
	}
	return a
}

func ladderBot(t *testing.T, level string, approvals ApprovalsSnapshot) Bot {
	t.Helper()
	bot := phaseBot(t, level, []string{"local_read", "patch", "scale"}, []TriggerSnapshot{ladderTrigger()})
	a, err := NewApprovals(approvals)
	if err != nil {
		t.Fatal(err)
	}
	bot.approvals = a
	return bot
}

func ladderIncident(t *testing.T, severity IncidentSeverity, namespaces ...string) Incident {
	t.Helper()
	incident, err := NewIncident("inc-1", "t", "triage", "g", "alertmanager", "Drift in shop", severity, []string{"fp"}, ladderNow)
	if err != nil {
		t.Fatal(err)
	}
	incident.RecordNamespaces(namespaces...)
	return incident
}

func TestDecideLadder(t *testing.T) {
	ctx := ladderBot(t, "act-within-policy", ApprovalsSnapshot{})
	trigger, _ := ctx.triggers.Named("apply")
	top := topAutonomy(t)
	if d := ctx.DecideLadder(trigger, ladderIncident(t, Sev2, "shop"), top, true); d.Grant == nil || d.Grant.Tool != "patch" || d.Grant.Namespace != "shop" || d.Grant.Runbook != "Drift" {
		t.Fatalf("decision %+v", d)
	}
	for name, c := range map[string]struct {
		bot      Bot
		incident Incident
		autonomy RunbookAutonomy
		known    bool
		want     string
	}{
		"no record yet":  {ctx, ladderIncident(t, Sev2, "shop"), RunbookAutonomy{}, false, "stands on act-with-approval"},
		"lower step":     {ctx, ladderIncident(t, Sev2, "shop"), newTestAutonomy(t, AutonomyActWithApproval), true, "stands on act-with-approval"},
		"no namespace":   {ctx, ladderIncident(t, Sev2), top, true, "0 namespaces"},
		"two namespaces": {ctx, ladderIncident(t, Sev2, "shop", "other"), top, true, "2 namespaces"},
		"outside":        {ctx, ladderIncident(t, Sev2, "other"), top, true, "outside the bounds"},
		"bot lowered":    {ladderBot(t, "act-with-approval", ApprovalsSnapshot{}), ladderIncident(t, Sev2, "shop"), top, true, "bot level"},
		"sev1 in prod":   {ladderBot(t, "act-within-policy", ApprovalsSnapshot{Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Namespaces: []string{"shop"}}}), ladderIncident(t, Sev1, "shop"), top, true, "sev1 in production"},
		"two persons":    {ladderBot(t, "act-within-policy", ApprovalsSnapshot{Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Severities: []string{"sev2"}}}), ladderIncident(t, Sev2, "shop"), top, true, "two persons"},
	} {
		d := c.bot.DecideLadder(trigger, c.incident, c.autonomy, c.known)
		if d.Grant != nil || !d.Laddered || !strings.Contains(d.Reason, c.want) {
			t.Errorf("%s: %+v", name, d)
		}
	}
	other := ladderIncident(t, Sev2, "shop")
	other.title = "Other in shop"
	if d := ctx.DecideLadder(trigger, other, top, true); !strings.Contains(d.Reason, "not on the ladder") {
		t.Errorf("another runbook: %+v", d)
	}
	nameless := ladderIncident(t, Sev2, "shop")
	nameless.title = "a group key"
	if d := ctx.DecideLadder(trigger, nameless, top, true); !strings.Contains(d.Reason, "names no") {
		t.Errorf("no runbook: %+v", d)
	}
	plain, _ := NewTrigger(TriggerSnapshot{Type: "incident", Phase: "apply"})
	if d := ctx.DecideLadder(plain, other, top, true); d.Laddered || d.Grant != nil {
		t.Errorf("no ladder: %+v", d)
	}
}

// The grant narrows the phase: the runtime's reads and the one write,
// held to the namespace, pinned on the task and read back.
func TestAGrantIsPinnedAndHeldToItsBound(t *testing.T) {
	bot := ladderBot(t, "act-within-policy", ApprovalsSnapshot{})
	trigger, _ := bot.triggers.Named("apply")
	decision := bot.DecideLadder(trigger, ladderIncident(t, Sev2, "shop"), topAutonomy(t), true)
	policy, err := bot.PolicyFor(trigger.Ref()).Granted(*decision.Grant)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Level() != AutonomyActWithinPolicy || !policy.Unattended() || strings.Join(policy.Tools().Names(), ",") != "local_read,patch" {
		t.Fatalf("policy %+v", policy)
	}
	task, _ := NewTask("task-1", bot.id, "o", "k", "c", ladderNow)
	if err := task.Attribute(trigger.Ref(), nil); err != nil {
		t.Fatal(err)
	}
	if err := task.ForIncident("inc-1"); err != nil {
		t.Fatal(err)
	}
	task.Pin(policy)
	restored, err := RestoreTask(task.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	effective, _, err := bot.PolicyOf(restored)
	if err != nil || !effective.Unattended() || effective.Level() != AutonomyActWithinPolicy {
		t.Fatalf("effective %+v %v", effective, err)
	}
	allow := effective.Rules().Snapshot().Allow
	if len(allow) != 1 || allow[0].Tool != "patch" || allow[0].Arguments["namespace"] != "shop" {
		t.Fatalf("rules %+v", allow)
	}

	// A bot lowered since keeps the bound and the card.
	lowered := ladderBot(t, "act-with-approval", ApprovalsSnapshot{})
	down, _, err := lowered.PolicyOf(restored)
	if err != nil || down.Unattended() || down.Level() != AutonomyActWithApproval {
		t.Fatalf("down %+v %v", down, err)
	}
	if allow := down.Rules().Snapshot().Allow; len(allow) != 1 || allow[0].Arguments["namespace"] != "shop" {
		t.Fatalf("rules %+v", allow)
	}
}

func TestRestoreLadderGrantRefuses(t *testing.T) {
	for _, s := range []*PinnedLadderSnapshot{{Runbook: "", Tool: "patch", Namespace: "a"}, {Runbook: "A", Tool: "", Namespace: "a"}, {Runbook: "A", Tool: "patch", Namespace: "Bad ns"}} {
		if _, err := restoreLadderGrant(s); err == nil {
			t.Errorf("%+v passed", s)
		}
	}
	if grant, err := restoreLadderGrant(nil); err != nil || grant != nil {
		t.Errorf("%v %v", grant, err)
	}
}

func phaseBot(t *testing.T, level string, tools []string, triggers []TriggerSnapshot) Bot {
	t.Helper()
	bot := sampleBot(t)
	allowed, err := NewAllowedTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	bot.allowedTools = allowed
	parsed, err := ParseAutonomyLevel(level)
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Govern(parsed, ToolRules{}); err != nil {
		t.Fatal(err)
	}
	wired, err := NewTriggers(triggers)
	if err != nil {
		t.Fatal(err)
	}
	bot.Wire(wired, bot.Placement())
	return bot
}
