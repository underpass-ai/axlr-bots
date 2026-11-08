package domain

import (
	"strings"
	"testing"
	"time"
)

var ladderNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func newTestAutonomy(t *testing.T, start AutonomyLevel) RunbookAutonomy {
	t.Helper()
	autonomy, err := NewRunbookAutonomy("t", "fix", "Drift", "shop", start, ladderNow)
	if err != nil {
		t.Fatal(err)
	}
	return autonomy
}

func record(t *testing.T, a *RunbookAutonomy, signal AutonomySignal, incident IncidentID, cycle int, actor string) AutonomyChange {
	t.Helper()
	change, err := a.Record(signal, incident, cycle, actor, DefaultAutonomyThresholds(), ladderNow)
	if err != nil {
		t.Fatal(err)
	}
	return change
}

// Reaching act-within-policy takes all three counters.
func TestTheTopStepTakesAllThreeCounters(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	for n := 1; n <= 20; n++ {
		record(t, &a, SignalProposalApproved, "inc-a", n, "tirso")
		record(t, &a, SignalApplyVerified, "inc-a", n, "")
	}
	if a.Step() != AutonomyActWithApproval || a.Earned().Proposals != 20 || a.Earned().Applies != 20 {
		t.Fatalf("no diagnoses, step %s, earned %+v", a.Step(), a.Earned())
	}
	for n := 1; n <= 4; n++ {
		record(t, &a, SignalDiagnosisCorrect, "inc-a", n, "tirso")
	}
	if a.Step() != AutonomyActWithApproval {
		t.Fatalf("step %s after four diagnoses", a.Step())
	}
	change := record(t, &a, SignalDiagnosisCorrect, "inc-a", 5, "tirso")
	if a.Step() != AutonomyActWithinPolicy || !change.Moved() || change.To != AutonomyActWithinPolicy {
		t.Fatalf("step %s, change %+v", a.Step(), change)
	}
	if !a.MayActAlone(DefaultAutonomyThresholds()) {
		t.Fatal("may not act alone")
	}
}

// D13: a cycle counts once; each cycle of one incident counts.
func TestEachCycleCountsOnceAndEveryCycleCounts(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	for n := 1; n <= 20; n++ {
		record(t, &a, SignalProposalApproved, "inc-a", n, "tirso")
		if change := record(t, &a, SignalProposalApproved, "inc-a", n, "tirso"); change.Recorded {
			t.Fatal("recorded twice")
		}
	}
	if a.Totals().Proposals != 20 || len(a.History()) != 20 {
		t.Fatalf("totals %+v history %d", a.Totals(), len(a.History()))
	}
}

// Only a person: policy and unnamed deciders record nothing.
func TestOnlyAPersonIsCounted(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	for _, actor := range []string{"", "policy:autoApprove", "policy:team-event", "a b"} {
		for _, signal := range []AutonomySignal{SignalProposalApproved, SignalDiagnosisCorrect, SignalApplyDenied} {
			if change := record(t, &a, signal, "inc-a", 1, actor); change.Recorded {
				t.Fatalf("%s by %q recorded", signal, actor)
			}
		}
	}
	if a.Step() != AutonomyActWithApproval || len(a.History()) != 0 {
		t.Fatalf("step %s history %+v", a.Step(), a.History())
	}
}

// A green verify counts only for a cycle a person approved, as theirs.
func TestAGreenCountsOnlyAnApprovedCycle(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	if change := record(t, &a, SignalApplyVerified, "inc-a", 1, ""); change.Recorded {
		t.Fatal("a green nobody approved counted")
	}
	record(t, &a, SignalProposalApproved, "inc-a", 1, "tirso")
	record(t, &a, SignalApplyVerified, "inc-a", 1, "")
	if a.Totals().Applies != 1 || a.History()[1].Actor != "tirso" {
		t.Fatalf("totals %+v history %+v", a.Totals(), a.History())
	}
}

func TestLoweringKeepsTheHistoryAndRestartsTheCount(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	record(t, &a, SignalProposalApproved, "inc-a", 1, "tirso")
	if change := record(t, &a, SignalApplyDenied, "inc-a", 2, "tirso"); !change.Moved() || a.Step() != AutonomyPropose {
		t.Fatalf("change %+v step %s", change, a.Step())
	}
	history := a.History()
	if len(history) != 2 || history[1].EarnedBefore == nil || history[1].EarnedBefore.Proposals != 1 || history[1].From != "act-with-approval" || history[1].To != "propose" {
		t.Fatalf("history %+v", history)
	}
	if a.Totals().Proposals != 1 || a.Earned().Proposals != 0 {
		t.Fatalf("totals %+v earned %+v", a.Totals(), a.Earned())
	}
	record(t, &a, SignalVerifyRed, "inc-b", 1, "")
	if a.Step() != AutonomyObserve {
		t.Fatalf("step %s", a.Step())
	}
	record(t, &a, SignalApplyFailed, "inc-b", 2, "")
	if a.Step() != AutonomyObserve {
		t.Fatalf("under the floor: %s", a.Step())
	}
}

// A lowering asks for the history again: the next signal does not give
// the step back.
func TestALoweringIsNotUndoneByTheNextSignal(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	for n := 1; n <= 20; n++ {
		record(t, &a, SignalDiagnosisCorrect, "inc-a", n, "tirso")
		record(t, &a, SignalProposalApproved, "inc-a", n, "tirso")
		record(t, &a, SignalApplyVerified, "inc-a", n, "")
	}
	if a.Step() != AutonomyActWithinPolicy {
		t.Fatalf("step %s", a.Step())
	}
	record(t, &a, SignalApplyDenied, "inc-b", 1, "tirso")
	if a.Step() != AutonomyActWithApproval {
		t.Fatalf("step %s", a.Step())
	}
	record(t, &a, SignalDiagnosisCorrect, "inc-c", 1, "tirso")
	record(t, &a, SignalProposalApproved, "inc-c", 1, "tirso")
	record(t, &a, SignalApplyVerified, "inc-c", 1, "")
	if a.Step() != AutonomyActWithApproval || a.Totals().Applies != 21 {
		t.Fatalf("step %s totals %+v", a.Step(), a.Totals())
	}
}

// The lower rungs climb in the order of design §8.3.
func TestTheLowerStepsClimb(t *testing.T) {
	a := newTestAutonomy(t, AutonomyObserve)
	for n := 1; n <= 5; n++ {
		record(t, &a, SignalDiagnosisCorrect, "inc-a", n, "tirso")
	}
	if a.Step() != AutonomyPropose {
		t.Fatalf("step %s", a.Step())
	}
	for n := 1; n <= 10; n++ {
		record(t, &a, SignalProposalApproved, "inc-a", n, "tirso")
	}
	if a.Step() != AutonomyActWithApproval {
		t.Fatalf("step %s", a.Step())
	}
}

func TestADiagnosisIsMarkedOnce(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	record(t, &a, SignalDiagnosisWrong, "inc-a", 1, "tirso")
	if _, err := a.Record(SignalDiagnosisCorrect, "inc-a", 1, "tirso", DefaultAutonomyThresholds(), ladderNow); !strings.Contains(err.Error(), "marked") {
		t.Fatalf("err = %v", err)
	}
	if mark, ok := a.DiagnosisMark("inc-a", 1); !ok || mark != SignalDiagnosisWrong {
		t.Fatalf("mark %s %v", mark, ok)
	}
	if a.Totals().Diagnoses != 0 || a.Step() != AutonomyActWithApproval {
		t.Fatalf("a wrong diagnosis counted or lowered: %+v", a.Snapshot())
	}
	if _, ok := a.DiagnosisMark("inc-a", 2); ok {
		t.Fatal("marked in another cycle")
	}
}

func TestRecordRefusesBadSignals(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	for _, c := range []struct {
		signal   AutonomySignal
		incident IncidentID
		cycle    int
	}{{"odd", "inc-a", 1}, {SignalVerifyRed, "", 1}, {SignalVerifyRed, "inc-a", 0}} {
		if _, err := a.Record(c.signal, c.incident, c.cycle, "tirso", DefaultAutonomyThresholds(), ladderNow); err == nil {
			t.Fatalf("%+v passed", c)
		}
	}
}

func TestRunbookAutonomyRoundTrip(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	record(t, &a, SignalProposalApproved, "inc-a", 1, "tirso")
	record(t, &a, SignalApplyDenied, "inc-b", 1, "tirso")
	back, err := RestoreRunbookAutonomy(a.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if back.Step() != a.Step() || back.Totals() != a.Totals() || back.Earned() != a.Earned() || len(back.History()) != 2 || back.ID() != a.ID() || back.Runbook() != "Drift" || back.Namespace() != "shop" || back.BotID() != "fix" || back.TenantID() != "t" {
		t.Fatalf("back %+v", back.Snapshot())
	}
	if !back.Seen("inc-a", 1, SignalProposalApproved) {
		t.Fatal("the idempotency was lost")
	}
}

func TestRestoreRunbookAutonomyRefuses(t *testing.T) {
	a := newTestAutonomy(t, AutonomyActWithApproval)
	record(t, &a, SignalProposalApproved, "inc-a", 1, "tirso")
	for name, mutate := range map[string]func(*RunbookAutonomySnapshot){
		"tenant":      func(s *RunbookAutonomySnapshot) { s.TenantID = "" },
		"bot":         func(s *RunbookAutonomySnapshot) { s.BotID = "" },
		"runbook":     func(s *RunbookAutonomySnapshot) { s.Runbook = "" },
		"start":       func(s *RunbookAutonomySnapshot) { s.Start = "odd" },
		"step":        func(s *RunbookAutonomySnapshot) { s.Step = "odd" },
		"namespace":   func(s *RunbookAutonomySnapshot) { s.Namespace = "Bad Ns" },
		"id":          func(s *RunbookAutonomySnapshot) { s.ID = "ladder-other" },
		"updated":     func(s *RunbookAutonomySnapshot) { s.UpdatedAt = time.Time{} },
		"counters":    func(s *RunbookAutonomySnapshot) { s.Totals.Applies = -1 },
		"signal":      func(s *RunbookAutonomySnapshot) { s.History[0].Signal = "odd" },
		"incident":    func(s *RunbookAutonomySnapshot) { s.History[0].IncidentID = "" },
		"entry cycle": func(s *RunbookAutonomySnapshot) { s.History[0].Cycle = 0 },
	} {
		s := a.Snapshot()
		mutate(&s)
		if _, err := RestoreRunbookAutonomy(s); err == nil {
			t.Errorf("%s passed", name)
		}
	}
}

func TestNewRunbookAutonomyRefuses(t *testing.T) {
	if _, err := NewRunbookAutonomy("", "fix", "Drift", "shop", AutonomyActWithApproval, ladderNow); err == nil {
		t.Error("no tenant")
	}
	if _, err := NewRunbookAutonomy("t", "fix", "Drift", "shop", "", ladderNow); err == nil {
		t.Error("no start")
	}
	if _, err := NewRunbookAutonomy("t", "fix", "Drift", "shop", AutonomyActWithApproval, time.Time{}); err == nil {
		t.Error("no time")
	}
	if _, err := NewAutonomyID("Bad"); err == nil {
		t.Error("bad id")
	}
	if _, err := NewAutonomyID("task-1"); err == nil {
		t.Error("no prefix")
	}
	if _, err := NewAutonomyID(AutonomyIDOf("t", "fix", "Drift", "shop").String()); err != nil {
		t.Error(err)
	}
	if AutonomyIDOf("t", "fix", "Drift", "shop") == AutonomyIDOf("t", "fix", "Drift", "shop2") {
		t.Error("two namespaces, one id")
	}
}

func TestRunbookOf(t *testing.T) {
	for _, c := range []struct {
		source, title, want string
		ok                  bool
	}{
		{"alertmanager", "KubePodCrashLooping in shop", "KubePodCrashLooping", true},
		{"alertmanager", "KubePodCrashLooping", "KubePodCrashLooping", true},
		{"manual", "Drift in drift-lab", "Drift", true},
		{"cloudwatch", "HighCPU: Threshold crossed", "HighCPU", true},
		{"alertmanager", "group key with spaces", "", false},
		{"alertmanager", "", "", false},
	} {
		name, ok := RunbookOf(c.source, c.title)
		if ok != c.ok || name.String() != c.want {
			t.Errorf("%q: %q %v", c.title, name, ok)
		}
	}
	for _, bad := range []string{"", " ", "a b", "a/b", "a..b", strings.Repeat("a", 129)} {
		if _, err := NewRunbookName(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

func TestAutonomyThresholds(t *testing.T) {
	if s := DefaultAutonomyThresholds().Snapshot(); s.Diagnoses != 5 || s.Proposals != 10 || s.Applies != 20 {
		t.Fatalf("defaults %+v", s)
	}
	mixed, err := NewAutonomyThresholds(AutonomyThresholdsSnapshot{Applies: 30})
	if err != nil || mixed.Snapshot().Applies != 30 || mixed.Snapshot().Diagnoses != 5 {
		t.Fatalf("%+v %v", mixed.Snapshot(), err)
	}
	for _, bad := range []AutonomyThresholdsSnapshot{{Diagnoses: -1}, {Proposals: maxAutonomyThreshold + 1}} {
		if _, err := NewAutonomyThresholds(bad); err == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

func TestSignalParsing(t *testing.T) {
	for _, signal := range []AutonomySignal{SignalDiagnosisCorrect, SignalDiagnosisWrong, SignalProposalApproved, SignalApplyVerified, SignalApplyDenied, SignalVerifyRed, SignalApplyFailed} {
		if _, err := ParseAutonomySignal(signal.String()); err != nil {
			t.Error(err)
		}
	}
	if _, err := ParseAutonomySignal("odd"); err == nil {
		t.Error("odd passed")
	}
}
