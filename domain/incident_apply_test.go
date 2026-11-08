package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// proposedIncident is an incident through triage and fix in cycle 1:
// Proposed, with its apply task attached when withApply.
func proposedIncident(t *testing.T, withApply bool) Incident {
	t.Helper()
	i := sampleIncident(t)
	steps := []func() error{
		func() error { return i.AttachTask("task-t", t0) },
		func() error { return i.Diagnose("drift: replicas 1, desired 3", t0) },
		func() error { return i.AttachPhaseTask("task-f", "fix", IncidentPhaseFix, t0) },
		func() error { return i.Propose("PROPOSAL: kubectl apply -f desired/web.yaml", t0) },
	}
	if withApply {
		steps = append(steps, func() error { return i.AttachPhaseTask("task-a", "fix", IncidentPhaseApply, t0) })
	}
	for n, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", n, err)
		}
	}
	return i
}

func TestIncidentApplyThenVerifyThenResolve(t *testing.T) {
	var serr StateError
	i := proposedIncident(t, false)
	if err := i.StartApplying(t0); !errors.As(err, &serr) {
		t.Errorf("applying without an apply task: %v", err)
	}
	if err := i.AttachPhaseTask("task-v", "verify", IncidentPhaseVerify, t0); !errors.As(err, &serr) {
		t.Errorf("verify task while Proposed: %v", err)
	}
	if err := i.AttachPhaseTask("task-x", "fix", IncidentPhaseTriage, t0); !errors.As(err, &serr) {
		t.Errorf("triage through AttachPhaseTask: %v", err)
	}
	if err := i.AttachPhaseTask("task-a", "fix", IncidentPhaseApply, t0); err != nil || i.CurrentTaskID() != "task-a" {
		t.Fatalf("apply task: %v", err)
	}
	if err := i.AttachPhaseTask("task-a2", "fix", IncidentPhaseApply, t0); !errors.As(err, &serr) {
		t.Errorf("second apply task in a cycle: %v", err)
	}
	if err := i.Verify(t0); !errors.As(err, &serr) {
		t.Errorf("verify before applying: %v", err)
	}
	t1 := t0.Add(time.Minute)
	if err := i.StartApplying(t1); err != nil || i.Status() != IncidentApplying || !i.UpdatedAt().Equal(t1) {
		t.Fatalf("start applying: %v", err)
	}
	if err := i.StartApplying(t1); !errors.As(err, &serr) {
		t.Errorf("applying twice: %v", err)
	}
	if err := i.Deny("late", t1); !errors.As(err, &serr) {
		t.Errorf("deny while Applying: %v", err)
	}
	if err := i.Verify(t1); err != nil || i.Status() != IncidentVerifying {
		t.Fatalf("verify: %v", err)
	}
	if err := i.AttachPhaseTask("task-v", "verify", IncidentPhaseVerify, t1); err != nil || i.CurrentTaskID() != "task-v" {
		t.Fatalf("verify task: %v", err)
	}
	if err := i.Repeat([]string{"fp-a"}, t1); err != nil || i.Status() != IncidentVerifying {
		t.Errorf("repeat while Verifying: %v", err)
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || back.Status() != IncidentVerifying || len(back.Tasks()) != 4 || back.Tasks()[3].Phase() != IncidentPhaseVerify {
		t.Fatalf("round trip: %+v %v", back.Snapshot(), err)
	}
	if err := i.Resolve("alerts green", t1); err != nil || i.Status() != IncidentResolved {
		t.Fatalf("resolve from Verifying: %v", err)
	}
}

func TestIncidentDeniedTwiceEscalates(t *testing.T) {
	var serr StateError
	i := proposedIncident(t, true)
	t1 := t0.Add(time.Minute)
	if err := i.Deny("", t1); err == nil {
		t.Error("a denial without a reason")
	}
	if err := i.Deny("apply denied by ops: not now", t1); err != nil || i.Status() != IncidentOpen || i.Cycle() != 2 || i.Denials() != 1 || i.Reason() != "apply denied by ops: not now" {
		t.Fatalf("first denial: %v %+v", err, i.Snapshot())
	}
	if i.Proposal() == "" || i.Diagnosis() == "" {
		t.Error("the denial dropped the diagnosis or the proposal before a new triage")
	}
	if err := i.Deny("again", t1); !errors.As(err, &serr) {
		t.Errorf("deny while Open: %v", err)
	}
	if i.HasTask(IncidentPhaseTriage) {
		t.Error("the new cycle has a triage task already")
	}
	// The next alert's triage, fix and apply of cycle 2.
	for n, step := range []func() error{
		func() error { return i.AttachTask("task-t2", t1) },
		func() error { return i.Diagnose("drift again", t1) },
		func() error { return i.AttachPhaseTask("task-f2", "fix", IncidentPhaseFix, t1) },
		func() error { return i.Propose("PROPOSAL: the same", t1) },
		func() error { return i.AttachPhaseTask("task-a2", "fix", IncidentPhaseApply, t1) },
	} {
		if err := step(); err != nil {
			t.Fatalf("cycle 2 step %d: %v", n, err)
		}
	}
	if err := i.Deny(strings.Repeat("approval expired ", 58), t1); err != nil || i.Status() != IncidentEscalated || i.Denials() != IncidentMaxDenials || !strings.HasSuffix(i.Reason(), "left to a person)") || len(i.Reason()) > MaxIncidentReasonLength {
		t.Fatalf("second denial: %v %+v", err, i.Snapshot())
	}
	if i.CanRetry() {
		t.Error("an incident denied twice is retried")
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || back.Denials() != IncidentMaxDenials || back.Status() != IncidentEscalated {
		t.Fatalf("round trip: %v", err)
	}
	if err := i.Resolve("by hand", t1); err != nil {
		t.Fatal(err)
	}
	if err := i.Reopen([]string{"fp-a"}, t1.Add(time.Minute)); err != nil || i.Denials() != 0 {
		t.Errorf("a reopening keeps the denials: %v %d", err, i.Denials())
	}
}

func TestIncidentSentBackByAFailedApplyOrARedVerify(t *testing.T) {
	var serr StateError
	i := proposedIncident(t, true)
	t1 := t0.Add(time.Minute)
	// The apply task failed before anyone approved it.
	if err := i.SendBack("apply task task-a Failed: node lost", t1); err != nil || i.Status() != IncidentOpen || i.Cycle() != 2 || i.Denials() != 0 {
		t.Fatalf("send back from Proposed: %v", err)
	}
	if err := i.SendBack("again", t1); !errors.As(err, &serr) {
		t.Errorf("send back while Open: %v", err)
	}
	i = proposedIncident(t, true)
	_ = i.StartApplying(t1)
	if err := i.SendBack("", t1); err == nil {
		t.Error("sent back without a reason")
	}
	if err := i.SendBack("apply failed, rolled back", t1); err != nil || i.Status() != IncidentOpen {
		t.Fatalf("send back from Applying: %v", err)
	}
	i = proposedIncident(t, true)
	_ = i.StartApplying(t1)
	_ = i.Verify(t1)
	if err := i.SendBack("verify: still red", t1); err != nil || i.Status() != IncidentOpen || i.Cycle() != 2 {
		t.Fatalf("send back from Verifying: %v", err)
	}
	i = proposedIncident(t, true)
	_ = i.StartApplying(t1)
	_ = i.Verify(t1)
	if err := i.Escalate("verify task Failed", t1); err != nil || i.Status() != IncidentEscalated {
		t.Fatalf("escalate from Verifying: %v", err)
	}
	if err := i.Escalate("twice", t1); !errors.As(err, &serr) {
		t.Errorf("escalate twice: %v", err)
	}
}

func TestIncidentRestoresTheNewStatuses(t *testing.T) {
	i := proposedIncident(t, true)
	_ = i.StartApplying(t0)
	for name, change := range map[string]func(s *IncidentSnapshot){
		"applying without a proposal": func(s *IncidentSnapshot) { s.Proposal = "" },
		"verifying without a proposal": func(s *IncidentSnapshot) {
			s.Status = "Verifying"
			s.Proposal = ""
		},
		"negative denials":  func(s *IncidentSnapshot) { s.Denials = -1 },
		"too many denials":  func(s *IncidentSnapshot) { s.Denials = IncidentMaxDenials + 1 },
		"unknown task kind": func(s *IncidentSnapshot) { s.Tasks[2].Phase = "rollback" },
	} {
		s := i.Snapshot()
		change(&s)
		if _, err := RestoreIncident(s); err == nil {
			t.Errorf("%s: restored", name)
		}
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || back.Status() != IncidentApplying {
		t.Fatalf("applying: %v", err)
	}
	for _, status := range []IncidentStatus{IncidentApplying, IncidentVerifying} {
		if parsed, err := ParseIncidentStatus(status.String()); err != nil || parsed != status || !status.HasProposal() {
			t.Errorf("%s: %v", status, err)
		}
	}
	if IncidentOpen.HasProposal() || IncidentEscalated.HasProposal() {
		t.Error("Open or Escalated stands on a proposal")
	}
	for phase, status := range map[IncidentPhase]IncidentStatus{
		IncidentPhaseTriage: IncidentOpen, IncidentPhaseFix: IncidentDiagnosed, IncidentPhaseApply: IncidentProposed, IncidentPhaseVerify: IncidentVerifying,
	} {
		if phase.LaunchedAt() != status {
			t.Errorf("%s launched at %s", phase, phase.LaunchedAt())
		}
		if parsed, err := ParseIncidentPhase(phase.String()); err != nil || parsed != phase {
			t.Errorf("%s: %v", phase, err)
		}
	}
}

func TestVerifyVerdict(t *testing.T) {
	for answer, want := range map[string]VerifyVerdict{
		"VERDICT: red\nthe alert still fires":       VerdictRed,
		"checked the rollout\n  verdict:  GREEN.  ": VerdictGreen,
		"Verdict: **red**":                          VerdictRed,
		"VERDICT: maybe\nVERDICT: red":              VerdictNone,
		"the rollout is fine":                       VerdictNone,
		"":                                          VerdictNone,
		"VERDICT":                                   VerdictNone,
		"no verdict here, VERDICT: red mid-sentence": VerdictNone,
	} {
		if got := ParseVerifyVerdict(answer); got != want {
			t.Errorf("%q = %q, want %q", answer, got, want)
		}
	}
	if !VerdictRed.IsRed() || VerdictGreen.IsRed() || VerdictNone.IsRed() || VerdictGreen.String() != "green" {
		t.Error("verdict predicates")
	}
}

func TestIncidentNeedsAPersonOnlyWhereItStands(t *testing.T) {
	i := proposedIncident(t, true)
	if !i.NeedsPerson(true) || i.NeedsPerson(false) {
		t.Error("a waiting apply of a Proposed incident")
	}
	_ = i.StartApplying(t0)
	if !i.NeedsPerson(true) {
		t.Error("a second wait while Applying")
	}
	i = proposedIncident(t, true)
	_ = i.Deny("no", t0)
	if i.NeedsPerson(true) {
		t.Error("the denied apply's next wait needs a person")
	}
	if fresh := sampleIncident(t); fresh.NeedsPerson(true) {
		t.Error("an incident without a task")
	}
	triage := sampleIncident(t)
	_ = triage.AttachTask("task-t", t0)
	if !triage.NeedsPerson(true) {
		t.Error("a waiting triage of an Open incident")
	}
}
