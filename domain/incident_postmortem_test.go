package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// resolvedIncident is a sev2 incident that resolved in cycle 1.
func resolvedIncident(t *testing.T) Incident {
	t.Helper()
	i := sampleIncident(t)
	if err := i.AttachTask("task-1", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.Resolve("green", t0); err != nil {
		t.Fatal(err)
	}
	return i
}

func wantState(t *testing.T, i Incident, want IncidentPostmortemState) IncidentPostmortem {
	t.Helper()
	p, ok := i.Postmortem()
	if !ok || p.State() != want {
		t.Fatalf("postmortem = %+v, want %s", p.Snapshot(), want)
	}
	return p
}

func TestAPostmortemIsWrittenApprovedAndKeptThroughARestore(t *testing.T) {
	i := resolvedIncident(t)
	if _, ok := i.Postmortem(); ok || !i.PostmortemDue() {
		t.Fatalf("before: due %v", i.PostmortemDue())
	}
	if err := i.AttachPhaseTask("task-x", "scribe", IncidentPhasePostmortem, t0); err == nil {
		t.Error("the postmortem went through the other phases' attach")
	}
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	if i.PostmortemDue() || i.CurrentTaskID() != "task-2" || i.Status() != IncidentResolved {
		t.Fatalf("after launch: %+v", i.Snapshot())
	}
	var state StateError
	if err := i.AttachPostmortemTask("task-3", "scribe", t0); !errors.As(err, &state) {
		t.Errorf("second launch: %v", err)
	}
	if err := i.ApprovePostmortem("tirso", t0); !errors.As(err, &state) {
		t.Errorf("approve while Writing: %v", err)
	}
	if err := i.RecordPostmortem("task-9", "# text", t0); !errors.As(err, &state) {
		t.Errorf("another task wrote it: %v", err)
	}
	if err := i.FailPostmortem("task-9", "boom", t0); !errors.As(err, &state) {
		t.Errorf("another task failed it: %v", err)
	}
	if err := i.RecordPostmortem("task-2", "  ", t0); err == nil {
		t.Error("an empty text was stored")
	}
	if err := i.RecordPostmortem("task-2", "# Postmortem\n\nCause.", t0); err != nil {
		t.Fatal(err)
	}
	p := wantState(t, i, PostmortemDraft)
	if p.AuthorTaskID() != "task-2" || p.Markdown() != "# Postmortem\n\nCause." || p.BotID() != "scribe" || p.TaskID() != "task-2" || p.Cycle() != 1 {
		t.Errorf("draft: %+v", p.Snapshot())
	}
	at := t0.Add(time.Hour)
	if err := i.ApprovePostmortem("tirso", at); err != nil {
		t.Fatal(err)
	}
	p = wantState(t, i, PostmortemApproved)
	if p.ApprovedBy() != "tirso" || p.Reason() != "" {
		t.Errorf("approved: %+v", p.Snapshot())
	}
	if err := i.ApprovePostmortem("again", at); !errors.As(err, &state) {
		t.Errorf("approved twice: %v", err)
	}
	if err := i.SendBackPostmortem("tirso", "no", at); !errors.As(err, &state) {
		t.Errorf("sent back once approved: %v", err)
	}
	restored, err := RestoreIncident(i.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got := wantState(t, restored, PostmortemApproved); got.Snapshot().Markdown != p.Markdown() || !got.Snapshot().ApprovedAt.Equal(at) {
		t.Errorf("restored: %+v", got.Snapshot())
	}
}

func TestSendingAPostmortemBackIsCappedAndLeavesItToAPerson(t *testing.T) {
	i := resolvedIncident(t)
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.RecordPostmortem("task-2", "draft one", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.SendBackPostmortem("tirso", "   ", t0); err == nil {
		t.Error("a send-back without a note")
	}
	if err := i.SendBackPostmortem("tirso", "add the owner", t0); err != nil {
		t.Fatal(err)
	}
	p := wantState(t, i, PostmortemSentBack)
	if p.SendBacks() != 1 || p.Note() != "add the owner" || p.ReviewedBy() != "tirso" || !i.PostmortemDue() {
		t.Fatalf("sent back: %+v", p.Snapshot())
	}
	// A launch that did not start: sending back again replaces the note
	// and does not count.
	if err := i.SendBackPostmortem("oncall", "and the date", t0); err != nil {
		t.Fatal(err)
	}
	if p = wantState(t, i, PostmortemSentBack); p.SendBacks() != 1 || p.Note() != "and the date" {
		t.Fatalf("again: %+v", p.Snapshot())
	}
	if err := i.AttachPostmortemTask("task-3", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	// The rewrite keeps the draft it rewrites; a cycle holds both tasks,
	// and a restore accepts them.
	if p = wantState(t, i, PostmortemWriting); p.Markdown() != "draft one" || p.AuthorTaskID() != "task-2" || p.TaskID() != "task-3" {
		t.Fatalf("rewriting: %+v", p.Snapshot())
	}
	if _, err := RestoreIncident(i.Snapshot()); err != nil {
		t.Fatalf("two postmortem tasks in a cycle: %v", err)
	}
	if err := i.FailPostmortem("task-3", "the model was down", t0); err != nil {
		t.Fatal(err)
	}
	if p = wantState(t, i, PostmortemFailed); p.Reason() != "the model was down" || p.Markdown() != "draft one" {
		t.Fatalf("failed: %+v", p.Snapshot())
	}
	var state StateError
	if err := i.ApprovePostmortem("tirso", t0); !errors.As(err, &state) {
		t.Errorf("approved a failed rewrite: %v", err)
	}
	if i.Status() != IncidentResolved {
		t.Fatalf("a failure moved the incident: %s", i.Status())
	}
	if err := i.SendBackPostmortem("tirso", "once more", t0); err != nil {
		t.Fatal(err)
	}
	if p = wantState(t, i, PostmortemLeftToPerson); p.SendBacks() != IncidentMaxPostmortemSendBacks || i.PostmortemDue() {
		t.Fatalf("left: %+v", p.Snapshot())
	}
	if err := i.SendBackPostmortem("tirso", "again", t0); !errors.As(err, &state) {
		t.Errorf("sent back past the cap: %v", err)
	}
	if _, err := RestoreIncident(i.Snapshot()); err != nil {
		t.Fatalf("left to a person: %v", err)
	}
	// The person may still take the last draft.
	if err := i.ApprovePostmortem("tirso", t0); err != nil {
		t.Fatal(err)
	}
	wantState(t, i, PostmortemApproved)
}

func TestApostmortemLeftWithoutADraftHasNothingToApprove(t *testing.T) {
	i := resolvedIncident(t)
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	for n, task := range []TaskID{"task-2", "task-3"} {
		if n > 0 {
			if err := i.AttachPostmortemTask(task, "scribe", t0); err != nil {
				t.Fatal(err)
			}
		}
		if err := i.FailPostmortem(task, "boom", t0); err != nil {
			t.Fatal(err)
		}
		if err := i.SendBackPostmortem("tirso", "try again", t0); err != nil {
			t.Fatal(err)
		}
	}
	wantState(t, i, PostmortemLeftToPerson)
	var state StateError
	if err := i.ApprovePostmortem("tirso", t0); !errors.As(err, &state) {
		t.Errorf("approved nothing: %v", err)
	}
}

// A reopening leaves the postmortem as it was, out of review; the next
// resolution writes another for its cycle.
func TestAReopeningLeavesThePostmortemOfItsCycleBehind(t *testing.T) {
	i := resolvedIncident(t)
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.RecordPostmortem("task-2", "first", t0); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(time.Minute)
	if err := i.Reopen([]string{"fp-a"}, t1); err != nil {
		t.Fatal(err)
	}
	var state StateError
	if err := i.ApprovePostmortem("tirso", t1); !errors.As(err, &state) {
		t.Errorf("approved while Open: %v", err)
	}
	if err := i.SendBackPostmortem("tirso", "x", t1); !errors.As(err, &state) {
		t.Errorf("sent back while Open: %v", err)
	}
	if err := i.RecordPostmortem("task-2", "late", t1); !errors.As(err, &state) {
		t.Errorf("a late answer while Open: %v", err)
	}
	if i.PostmortemDue() {
		t.Error("due while Open")
	}
	if p := wantState(t, i, PostmortemDraft); p.Markdown() != "first" {
		t.Errorf("kept: %+v", p.Snapshot())
	}
	if _, err := RestoreIncident(i.Snapshot()); err != nil {
		t.Fatalf("restore after a reopening: %v", err)
	}
	if err := i.Resolve("green again", t1); err != nil {
		t.Fatal(err)
	}
	if err := i.ApprovePostmortem("tirso", t1); !errors.As(err, &state) {
		t.Errorf("approved a draft of another cycle: %v", err)
	}
	if !i.PostmortemDue() {
		t.Fatal("not due in the new cycle")
	}
	if err := i.AttachPostmortemTask("task-4", "scribe", t1); err != nil {
		t.Fatal(err)
	}
	if p := wantState(t, i, PostmortemWriting); p.Markdown() != "" || p.Cycle() != 2 || p.SendBacks() != 0 {
		t.Errorf("new cycle: %+v", p.Snapshot())
	}
}

func TestARestoreRefusesABadPostmortem(t *testing.T) {
	i := resolvedIncident(t)
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.RecordPostmortem("task-2", "text", t0); err != nil {
		t.Fatal(err)
	}
	at := t0
	for name, spoil := range map[string]func(*IncidentPostmortemSnapshot){
		"state":       func(s *IncidentPostmortemSnapshot) { s.State = "Lost" },
		"task":        func(s *IncidentPostmortemSnapshot) { s.TaskID = "" },
		"bot":         func(s *IncidentPostmortemSnapshot) { s.BotID = "" },
		"cycle":       func(s *IncidentPostmortemSnapshot) { s.Cycle = 2 },
		"no cycle":    func(s *IncidentPostmortemSnapshot) { s.Cycle = 0 },
		"sendBacks":   func(s *IncidentPostmortemSnapshot) { s.SendBacks = IncidentMaxPostmortemSendBacks + 1 },
		"no text":     func(s *IncidentPostmortemSnapshot) { s.Markdown = "" },
		"long text":   func(s *IncidentPostmortemSnapshot) { s.Markdown = strings.Repeat("x", MaxIncidentPostmortemLength+1) },
		"no author":   func(s *IncidentPostmortemSnapshot) { s.AuthorTaskID = "" },
		"bad author":  func(s *IncidentPostmortemSnapshot) { s.AuthorTaskID = "no spaces" },
		"approver":    func(s *IncidentPostmortemSnapshot) { s.ApprovedBy, s.ApprovedAt = "tirso", &at },
		"no approver": func(s *IncidentPostmortemSnapshot) { s.State = "Approved" },
		"note":        func(s *IncidentPostmortemSnapshot) { s.Note = strings.Repeat("x", MaxIncidentReasonLength+1) },
		"reason":      func(s *IncidentPostmortemSnapshot) { s.Reason = strings.Repeat("x", MaxIncidentReasonLength+1) },
	} {
		s := i.Snapshot()
		spoil(s.Postmortem)
		if _, err := RestoreIncident(s); err == nil {
			t.Errorf("%s: restored", name)
		}
	}
	// A snapshot's postmortem is a copy.
	s := i.Snapshot()
	s.Postmortem.Markdown = "changed"
	if p, _ := i.Postmortem(); p.Markdown() != "text" {
		t.Error("the snapshot reached the incident")
	}
	if _, err := ParseIncidentPostmortemState(""); err == nil {
		t.Error("empty state parsed")
	}
}

func TestTheTriggerOfThePostmortemPhaseSaysWhichSeverities(t *testing.T) {
	scribe := func(severities ...string) (Trigger, error) {
		return NewTrigger(TriggerSnapshot{Type: "incident", Phase: "postmortem", Severities: severities})
	}
	byDefault, err := scribe()
	if err != nil || byDefault.Phase() != IncidentPhasePostmortem {
		t.Fatalf("%v %v", byDefault.Phase(), err)
	}
	for severity, want := range map[IncidentSeverity]bool{Sev1: true, Sev2: true, Sev3: false, Sev4: false} {
		if byDefault.WritesPostmortemFor(severity) != want {
			t.Errorf("default %s: %v", severity, !want)
		}
	}
	if byDefault.Snapshot().Severities != nil {
		t.Errorf("the default was written back: %v", byDefault.Snapshot().Severities)
	}
	lab, err := scribe("sev1", "sev3")
	if err != nil || !lab.WritesPostmortemFor(Sev3) || lab.WritesPostmortemFor(Sev2) || len(lab.Snapshot().Severities) != 2 {
		t.Fatalf("lab: %v %v", lab.Snapshot().Severities, err)
	}
	for name, bad := range map[string][]string{"empty": {""}, "unknown": {"sev9"}, "repeat": {"sev1", "sev1"}} {
		if _, err := scribe(bad...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "incident", Phase: "fix", Severities: []string{"sev1"}}); err == nil {
		t.Error("the fix phase took severities")
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "manual", Severities: []string{"sev1"}}); err == nil {
		t.Error("a manual trigger took severities")
	}
	if IncidentPhasePostmortem.LaunchedAt() != IncidentResolved {
		t.Error("launched at")
	}
}

func TestAnOutputThatNamedNoEventHearsTheApproval(t *testing.T) {
	definition := OutputTargetSnapshot{Name: "chat", Type: "slack", URL: "https://hooks.example.com/x"}
	fresh, err := NewOutputTarget(definition)
	if err != nil {
		t.Fatal(err)
	}
	i := sampleIncident(t)
	if !fresh.Wants(EventIncidentPostmortemApproved, i) || fresh.Wants(EventIncidentPostmortemDrafted, i) {
		t.Error("the default")
	}
	for _, event := range outputEventsBeforePostmortem {
		definition.Events = append(definition.Events, event.String())
	}
	stored, err := NewOutputTarget(definition)
	if err != nil || !stored.Wants(EventIncidentPostmortemApproved, i) {
		t.Errorf("stored before postmortems: %v", err)
	}
	definition.Events = []string{"opened"}
	chosen, _ := NewOutputTarget(definition)
	if chosen.Wants(EventIncidentPostmortemApproved, i) {
		t.Error("a target that chose its events heard the approval")
	}
	if event, err := ParseOutputEvent("postmortem_drafted"); err != nil || event != EventIncidentPostmortemDrafted {
		t.Errorf("%v %v", event, err)
	}
}
