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
	approvals, err := NewApprovals(ApprovalsSnapshot{Approvers: []string{"tirso"}})
	if err != nil {
		t.Fatal(err)
	}
	bot.approvals = approvals
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
	a.BindTool("patch", ladderNow)
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
	if len(approvals.Approvers) == 0 {
		approvals.Approvers = []string{"tirso"}
	}
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
	incident.ObserveAlerts(AlertGroupUniform, incident.Title(), severity)
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
		"sev1":           {ctx, ladderIncident(t, Sev1, "shop"), top, true, "sev1 never"},
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

func TestALadderNeedsPeopleToApprove(t *testing.T) {
	bot := phaseBot(t, "act-within-policy", []string{"local_read", "patch", "scale"}, []TriggerSnapshot{ladderTrigger()})
	if err := bot.CheckTriggers(); err == nil || !strings.Contains(err.Error(), "approvers") {
		t.Errorf("no approvers: %v", err)
	}
	for _, machine := range []string{"bot:triage", "node:sre-rw", "trigger:apply"} {
		approvals, err := NewApprovals(ApprovalsSnapshot{Approvers: []string{machine}})
		if err != nil {
			t.Fatal(err)
		}
		bot.approvals = approvals
		if err := bot.CheckTriggers(); err == nil {
			t.Errorf("%s passed as a person", machine)
		}
	}
	for _, spelled := range []string{"Policy:x", "cli:policy:x", " POLICY:x"} {
		if !IsPolicyActor(spelled) || !IsMachineActor(spelled) {
			t.Errorf("%q is a policy", spelled)
		}
	}
	if IsMachineActor("tirso") || IsPolicyActor("cli:tirso") {
		t.Error("a person is a machine")
	}
}

func TestATitleMustBeTheRunbookExactly(t *testing.T) {
	bot := ladderBot(t, "act-within-policy", ApprovalsSnapshot{})
	trigger, _ := bot.triggers.Named("apply")
	for title, want := range map[string]bool{
		"Drift in shop":             true,
		"Drift in disguise in shop": false,
		"Drift":                     false,
		"Drift in other":            false,
	} {
		incident := ladderIncident(t, Sev2, "shop")
		incident.title = title
		if _, reason, ok := LadderUnitOf(trigger, incident); ok != want {
			t.Errorf("%q: %v %s", title, ok, reason)
		}
	}
	if name, ok := RunbookOf("alertmanager", "Drift in disguise in shop"); ok {
		t.Errorf("parsed as %s", name)
	}
	if _, ok := RunbookOf("cloudwatch", "Drift: reason"); ok {
		t.Error("a CloudWatch title named a runbook")
	}
}

func TestAMixedGroupHasNoUnit(t *testing.T) {
	bot := ladderBot(t, "act-within-policy", ApprovalsSnapshot{})
	trigger, _ := bot.triggers.Named("apply")
	incident := ladderIncident(t, Sev2, "shop")
	incident.ObserveAlerts(AlertGroupMixed, incident.Title(), Sev2)
	if _, _, ok := LadderUnitOf(trigger, incident); ok {
		t.Error("a mixed group has a unit")
	}
	incident.ObserveAlerts(AlertGroupUniform, incident.Title(), Sev2)
	if incident.AlertGroup() != AlertGroupMixed {
		t.Error("mixed is for life")
	}
	hand := ladderIncident(t, Sev2, "shop")
	hand.alerts = AlertGroupUnknown
	if d := bot.DecideLadder(trigger, hand, topAutonomy(t), true); d.Grant != nil || !strings.Contains(d.Reason, "not known") {
		t.Errorf("a hand-opened incident: %+v", d)
	}
	later := ladderIncident(t, Sev3, "shop")
	later.ObserveAlerts(AlertGroupUniform, later.Title(), Sev1)
	if later.AlertGroup() != AlertGroupMixed {
		t.Error("a worse severity later did not make it mixed")
	}
	renamed := ladderIncident(t, Sev3, "shop")
	renamed.ObserveAlerts(AlertGroupUniform, "Other in shop", Sev3)
	if renamed.AlertGroup() != AlertGroupMixed {
		t.Error("another alert later did not make it mixed")
	}
	restored, err := RestoreIncident(later.Snapshot())
	if err != nil || restored.AlertGroup() != AlertGroupMixed {
		t.Errorf("restored %s %v", restored.AlertGroup(), err)
	}
	bad := later.Snapshot()
	bad.AlertGroup = "odd"
	if _, err := RestoreIncident(bad); err == nil {
		t.Error("an odd group restored")
	}
}

func TestAlertmanagerUniform(t *testing.T) {
	alert := func(name, severity string) AlertmanagerAlert {
		return AlertmanagerAlert{Labels: map[string]string{"alertname": name, "severity": severity}}
	}
	for name, c := range map[string]struct {
		alerts []AlertmanagerAlert
		want   bool
	}{
		"one":            {[]AlertmanagerAlert{alert("A", "warning")}, true},
		"same":           {[]AlertmanagerAlert{alert("A", "warning"), alert("A", "info")}, true},
		"another name":   {[]AlertmanagerAlert{alert("A", "warning"), alert("B", "warning")}, false},
		"worse severity": {[]AlertmanagerAlert{alert("A", "warning"), alert("A", "critical")}, false},
		"no name at all": {[]AlertmanagerAlert{{Labels: map[string]string{}}}, false},
	} {
		if got := (AlertmanagerPayload{Alerts: c.alerts}).Uniform(); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestOutstandingAndToolAndLowering(t *testing.T) {
	a := topAutonomy(t)
	a.Begin("inc-b", 1, ladderNow)
	a.Begin("inc-b", 1, ladderNow)
	if len(a.Outstanding()) != 1 || !a.HoldsOpen("inc-c", 1) || a.HoldsOpen("inc-b", 1) {
		t.Fatalf("outstanding %+v", a.Outstanding())
	}
	if a.Abandon("inc-x", 1, ladderNow) {
		t.Fatal("abandoned what was not outstanding")
	}
	a.Begin("inc-c", 1, ladderNow)
	record(t, &a, SignalApplyVerified, "inc-b", 1, "")
	if len(a.Outstanding()) != 1 {
		t.Fatalf("a green left it: %+v", a.Outstanding())
	}
	record(t, &a, SignalVerifyRed, "inc-c", 1, "")
	if len(a.Outstanding()) != 0 {
		t.Fatalf("a red left it: %+v", a.Outstanding())
	}
	if err := a.Lower("policy:x", "", ladderNow); err == nil {
		t.Fatal("a policy lowered")
	}
	a.Begin("inc-d", 1, ladderNow)
	from := a.Step()
	if err := a.Lower("tirso", "stop", ladderNow); err != nil {
		t.Fatal(err)
	}
	last := a.History()[len(a.History())-1]
	if len(a.Outstanding()) != 0 || a.Step() == from || last.Signal != "lowered-by-person" || last.Reason != "stop" {
		t.Fatalf("lower: %+v", last)
	}
	back, err := RestoreRunbookAutonomy(a.Snapshot())
	if err != nil || back.Tool() != "patch" {
		t.Fatalf("restore %v %s", err, back.Tool())
	}

	b := topAutonomy(t)
	if b.BindTool("patch", ladderNow) {
		t.Fatal("the same tool changed it")
	}
	if !b.BindTool("scale", ladderNow) || b.Step() != AutonomyActWithApproval || b.Totals() != (AutonomyCounters{}) || b.MayActAlone(DefaultAutonomyThresholds()) {
		t.Fatalf("after a new tool: %+v", b.Snapshot())
	}

	broken := a.Snapshot()
	broken.Tool = "Bad Tool"
	if _, err := RestoreRunbookAutonomy(broken); err == nil {
		t.Error("a bad tool restored")
	}
	broken = a.Snapshot()
	broken.Outstanding = []AutonomyOutstanding{{IncidentID: "", Cycle: 1}}
	if _, err := RestoreRunbookAutonomy(broken); err == nil {
		t.Error("a bad outstanding restored")
	}
	broken.Outstanding = []AutonomyOutstanding{{IncidentID: "inc-a", Cycle: 0}}
	if _, err := RestoreRunbookAutonomy(broken); err == nil {
		t.Error("an outstanding of no cycle restored")
	}
}

func TestTheHistoryIsCappedAndKeepsTheLowerings(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	record(t, &a, SignalApplyDenied, "inc-low", 1, "tirso")
	for n := 1; n <= maxAutonomyHistory+50; n++ {
		record(t, &a, SignalDiagnosisWrong, "inc-a", n, "tirso")
	}
	history := a.History()
	if len(history) != maxAutonomyHistory || history[0].Signal != "apply-denied" {
		t.Fatalf("len %d first %+v", len(history), history[0])
	}
	all := make([]AutonomyEntry, maxAutonomyHistory+3)
	for n := range all {
		all[n] = AutonomyEntry{Signal: "apply-denied", From: "a", IncidentID: "inc-a", Cycle: n + 1, At: ladderNow}
	}
	if kept := capHistory(all); len(kept) != maxAutonomyHistory || kept[0].Cycle != 4 {
		t.Fatalf("every line moved: %d %+v", len(kept), kept[0])
	}
}

func TestCountWritesAndEffectiveTool(t *testing.T) {
	calls := []ToolCall{{Name: "local_read", Status: "completed"}, {Name: "patch", Status: "completed"}, {Name: "axlr_call_tool", Status: "failed"}, {Name: "patch", Status: "denied"}, {Name: "axlr_call_tool", Status: "pending"}}
	if got := CountWrites(calls, "patch"); got != 2 {
		t.Errorf("writes %d", got)
	}
	bridged := PendingDecision{Tool: "axlr_call_tool", Arguments: []byte(`{"name":"patch","arguments":{}}`)}
	if bridged.EffectiveTool() != "patch" {
		t.Errorf("tool %s", bridged.EffectiveTool())
	}
	for _, plain := range []PendingDecision{{Tool: "scale"}, {Tool: "axlr_call_tool", Arguments: []byte(`not json`)}, {Tool: "axlr_call_tool", Arguments: []byte(`{}`)}} {
		if plain.EffectiveTool() != plain.Tool {
			t.Errorf("%+v: %s", plain, plain.EffectiveTool())
		}
	}
}

func TestRevokeAloneKeepsTheUnitAtTheCard(t *testing.T) {
	bot := ladderBot(t, "act-within-policy", ApprovalsSnapshot{})
	trigger, _ := bot.triggers.Named("apply")
	decision := bot.DecideLadder(trigger, ladderIncident(t, Sev2, "shop"), topAutonomy(t), true)
	policy, err := bot.PolicyFor(trigger.Ref()).Granted(*decision.Grant)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := NewTask("task-1", bot.id, "o", "k", "c", ladderNow)
	_ = task.Attribute(trigger.Ref(), nil)
	_ = task.ForIncident("inc-1")
	task.Pin(policy)
	if !task.RevokeAlone() || task.RevokeAlone() {
		t.Fatal("revoke")
	}
	pinned, _ := task.Pinned()
	unit, ok := pinned.LadderUnit()
	if pinned.Unattended() || pinned.Level() != AutonomyActWithApproval || !ok || unit.Tool != "patch" || unit.Alone {
		t.Fatalf("pinned %+v %+v", pinned, unit)
	}
	tracked := bot.PolicyFor(trigger.Ref()).Tracked(*decision.Unit)
	if tracked.Unattended() || tracked.Level() != AutonomyActWithApproval {
		t.Fatalf("tracked %+v", tracked)
	}
	if _, ok := bot.PolicyFor(TriggerRef{}).LadderUnit(); ok {
		t.Fatal("a manual policy has a unit")
	}
}
