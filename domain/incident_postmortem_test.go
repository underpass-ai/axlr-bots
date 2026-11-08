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
	if err := i.ApprovePostmortem("tirso", "task-2", t0); !errors.As(err, &state) {
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
	if err := i.ApprovePostmortem("tirso", "task-2", at); err != nil {
		t.Fatal(err)
	}
	p = wantState(t, i, PostmortemApproved)
	if p.ApprovedBy() != "tirso" || p.Reason() != "" {
		t.Errorf("approved: %+v", p.Snapshot())
	}
	if err := i.ApprovePostmortem("again", "task-2", at); !errors.As(err, &state) {
		t.Errorf("approved twice: %v", err)
	}
	if err := i.SendBackPostmortem("tirso", "no", "task-2", at); !errors.As(err, &state) {
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
	if err := i.SendBackPostmortem("tirso", "   ", "task-2", t0); err == nil {
		t.Error("a send-back without a note")
	}
	if err := i.SendBackPostmortem("tirso", "add the owner", "task-2", t0); err != nil {
		t.Fatal(err)
	}
	p := wantState(t, i, PostmortemSentBack)
	if p.SendBacks() != 1 || p.Note() != "add the owner" || p.ReviewedBy() != "tirso" || !i.PostmortemDue() {
		t.Fatalf("sent back: %+v", p.Snapshot())
	}
	// A launch that did not start: sending back again replaces the note
	// and does not count.
	if err := i.SendBackPostmortem("oncall", "and the date", "task-2", t0); err != nil {
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
	if err := i.ApprovePostmortem("tirso", "task-2", t0); !errors.As(err, &state) {
		t.Errorf("approved a failed rewrite: %v", err)
	}
	if i.Status() != IncidentResolved {
		t.Fatalf("a failure moved the incident: %s", i.Status())
	}
	// A failure spends nothing: no text was reviewed.
	if err := i.SendBackPostmortem("tirso", "once more", "task-2", t0); err != nil {
		t.Fatal(err)
	}
	if p = wantState(t, i, PostmortemSentBack); p.SendBacks() != 1 {
		t.Fatalf("a failure counted: %+v", p.Snapshot())
	}
	if err := i.AttachPostmortemTask("task-4", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.RecordPostmortem("task-4", "draft two", t0); err != nil {
		t.Fatal(err)
	}
	if err := i.SendBackPostmortem("tirso", "no", "task-2", t0); !errors.As(err, &state) {
		t.Errorf("a stale send-back burnt the last one: %v", err)
	}
	if err := i.SendBackPostmortem("tirso", "no", "task-4", t0); err != nil {
		t.Fatal(err)
	}
	if p = wantState(t, i, PostmortemLeftToPerson); p.SendBacks() != IncidentMaxPostmortemSendBacks || i.PostmortemDue() {
		t.Fatalf("left: %+v", p.Snapshot())
	}
	if err := i.SendBackPostmortem("tirso", "again", "task-4", t0); !errors.As(err, &state) {
		t.Errorf("sent back past the cap: %v", err)
	}
	if _, err := RestoreIncident(i.Snapshot()); err != nil {
		t.Fatalf("left to a person: %v", err)
	}
	if err := i.ApprovePostmortem("tirso", "task-2", t0); !errors.As(err, &state) {
		t.Errorf("approved a text not read: %v", err)
	}
	// The person may still take the last draft.
	if err := i.ApprovePostmortem("tirso", "task-4", t0); err != nil {
		t.Fatal(err)
	}
	wantState(t, i, PostmortemApproved)
}

// Failures never leave it where nothing can move it.
func TestFailuresAreNeverADeadEnd(t *testing.T) {
	i := resolvedIncident(t)
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	for n, task := range []TaskID{"task-2", "task-3", "task-4"} {
		if n > 0 {
			if err := i.AttachPostmortemTask(task, "scribe", t0); err != nil {
				t.Fatal(err)
			}
		}
		if err := i.FailPostmortem(task, "boom", t0); err != nil {
			t.Fatal(err)
		}
		if err := i.SendBackPostmortem("tirso", "try again", "", t0); err != nil {
			t.Fatal(err)
		}
		if p := wantState(t, i, PostmortemSentBack); p.SendBacks() != 0 {
			t.Fatalf("counted: %+v", p.Snapshot())
		}
	}
}

// With none started, a send-back asks for one; a request has no task
// yet and a restore keeps it.
func TestASendBackAsksForAPostmortemNoneStarted(t *testing.T) {
	i := resolvedIncident(t)
	var state StateError
	if err := i.SendBackPostmortem("tirso", "please", "task-9", t0); !errors.As(err, &state) {
		t.Errorf("a text read of a postmortem that does not exist: %v", err)
	}
	if _, ok := i.Postmortem(); ok {
		t.Fatal("a refusal left a postmortem")
	}
	if err := i.SendBackPostmortem("tirso", "please", "", t0); err != nil {
		t.Fatal(err)
	}
	p := wantState(t, i, PostmortemSentBack)
	if p.TaskID() != "" || p.BotID() != "" || p.Note() != "please" || !i.PostmortemDue() {
		t.Fatalf("asked: %+v", p.Snapshot())
	}
	if _, err := RestoreIncident(i.Snapshot()); err != nil {
		t.Fatalf("restore a request: %v", err)
	}
	s := i.Snapshot()
	s.Postmortem.TaskID = "task-1"
	if _, err := RestoreIncident(s); err == nil {
		t.Error("a task without a bot")
	}
	open := sampleIncident(t)
	if err := open.SendBackPostmortem("tirso", "x", "", t0); !errors.As(err, &state) {
		t.Errorf("asked on an open incident: %v", err)
	}
	if err := i.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	if p := wantState(t, i, PostmortemWriting); p.Note() != "please" {
		t.Errorf("the note: %+v", p.Snapshot())
	}
}

// Moving a copy of an incident does not reach the original.
func TestACopyOfAnIncidentDoesNotShareItsPostmortem(t *testing.T) {
	i := resolvedIncident(t)
	if err := i.SendBackPostmortem("tirso", "please", "", t0); err != nil {
		t.Fatal(err)
	}
	launched := i
	if err := launched.AttachPostmortemTask("task-2", "scribe", t0); err != nil {
		t.Fatal(err)
	}
	wantState(t, i, PostmortemSentBack)
	if err := launched.FailPostmortem("task-2", "boom", t0); err != nil {
		t.Fatal(err)
	}
	copied := launched
	if err := copied.SendBackPostmortem("tirso", "again", "", t0); err != nil {
		t.Fatal(err)
	}
	wantState(t, launched, PostmortemFailed)
}

func TestWhatATaskWroteLosesControlsAndATitleItsLines(t *testing.T) {
	if got := IncidentText("ok \u009b2J\u009d text \x7f", 100); got != "ok 2J text" {
		t.Errorf("%q", got)
	}
	if !IncidentTextIsEmpty("\x01 \n\u0085\x7f") || IncidentTextIsEmpty(" x ") {
		t.Error("empty")
	}
	i, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "Disk\n\nfull\tnow", Sev2, nil, t0)
	if err != nil || i.Title() != "Disk  full now" {
		t.Errorf("%q %v", i.Title(), err)
	}
}

func TestEverySpellingOfAMachineNameIsOne(t *testing.T) {
	for _, name := range []string{"policy:x", "POLICY:x", "Policy:x", "cli:policy:x", "cli:POLICY:x", "trigger:am", "TRIGGER:am", "cli:trigger:am"} {
		if !IsMachineActor(name) {
			t.Errorf("%s", name)
		}
	}
	if IsMachineActor("tirso") || IsMachineActor("cli:tirso") || IsPolicyActor("trigger:am") {
		t.Error("a person")
	}
	if _, err := NewApprovals(ApprovalsSnapshot{Approvers: []string{"cli:POLICY:x"}}); err == nil {
		t.Error("a policy listed as an approver")
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
	if err := i.ApprovePostmortem("tirso", "task-2", t1); !errors.As(err, &state) {
		t.Errorf("approved while Open: %v", err)
	}
	if err := i.SendBackPostmortem("tirso", "x", "task-2", t1); !errors.As(err, &state) {
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
	if err := i.ApprovePostmortem("tirso", "task-2", t1); !errors.As(err, &state) {
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
	// Stored as absent, so it hears what the default comes to include.
	if fresh.Snapshot().Events != nil || len(fresh.Events()) != len(DefaultOutputEvents) {
		t.Errorf("stored %v, reads %v", fresh.Snapshot().Events, fresh.Events())
	}
	// Exactly the eight of before, named today: eight, stored as eight.
	for _, event := range DefaultOutputEvents[:len(DefaultOutputEvents)-1] {
		definition.Events = append(definition.Events, event.String())
	}
	named, err := NewOutputTarget(definition)
	if err != nil || named.Wants(EventIncidentPostmortemApproved, i) || len(named.Snapshot().Events) != len(DefaultOutputEvents)-1 {
		t.Errorf("named: %v %v", named.Snapshot().Events, err)
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
