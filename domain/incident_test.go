package domain

import (
	"errors"
	"testing"
	"time"
)

func sampleIncident(t *testing.T) Incident {
	t.Helper()
	incident, err := NewIncident("inc-1", "ops", "triage", `{}/{alertname="KubePodCrashLooping"}`, "alertmanager", "KubePodCrashLooping in shop", Sev2, []string{"fp-a", "fp-a", " fp-b "}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return incident
}

func TestIncidentLifecycle(t *testing.T) {
	i := sampleIncident(t)
	if i.Status() != IncidentOpen || i.Cycle() != 1 || len(i.Fingerprints()) != 2 || i.Fingerprints()[1] != "fp-b" || i.CurrentTaskID() != "" || i.Severity() != Sev2 {
		t.Fatalf("new incident: %+v", i.Snapshot())
	}
	var serr StateError
	if err := i.Diagnose("x", t0); err != nil || i.Status() != IncidentDiagnosed {
		t.Fatalf("diagnose: %v", err)
	}
	if err := i.Diagnose("twice", t0); !errors.As(err, &serr) {
		t.Errorf("second diagnose: %v", err)
	}
	t1 := t0.Add(time.Minute)
	if err := i.AttachTask("task-1", t1); err != nil || i.CurrentTaskID() != "task-1" || !i.UpdatedAt().Equal(t1) {
		t.Fatalf("attach: %v", err)
	}
	if err := i.AttachTask("task-2", t1); !errors.As(err, &serr) {
		t.Errorf("second task in a cycle: %v", err)
	}
	if err := i.Repeat([]string{"fp-c"}, t1); err != nil || i.Repeats() != 1 || len(i.Fingerprints()) != 3 || !i.LastSeenAt().Equal(t1) {
		t.Fatalf("repeat: %v", err)
	}
	if err := i.AddCost(0.25, t1); err != nil || i.CostUSD() != 0.25 {
		t.Fatalf("cost: %v", err)
	}
	if err := i.AddCost(-1, t1); err == nil {
		t.Error("negative cost added")
	}
	if i.CanReopen(t1) {
		t.Error("an open incident reopens")
	}
	if err := i.Reopen(nil, t1); !errors.As(err, &serr) {
		t.Errorf("reopen while open: %v", err)
	}
	if err := i.Resolve("alerts green", t1); err != nil || i.Status() != IncidentResolved || i.Reason() != "alerts green" {
		t.Fatalf("resolve: %v", err)
	}
	if at, ok := i.ResolvedAt(); !ok || !at.Equal(t1) {
		t.Error("resolved at not kept")
	}
	for _, attempt := range []func() error{
		func() error { return i.Resolve("", t1) },
		func() error { return i.Repeat(nil, t1) },
		func() error { return i.AttachTask("task-3", t1) },
		func() error { return i.Escalate("why", t1) },
	} {
		if err := attempt(); !errors.As(err, &serr) {
			t.Errorf("after resolve: %v", err)
		}
	}
	late := t1.Add(IncidentReopenWindow)
	if i.CanReopen(late) {
		t.Error("reopens at the window's end")
	}
	soon := t1.Add(IncidentReopenWindow - time.Second)
	if err := i.Reopen([]string{"fp-d"}, soon); err != nil || i.Status() != IncidentOpen || i.Cycle() != 2 || len(i.Fingerprints()) != 4 {
		t.Fatalf("reopen: %v %+v", err, i.Snapshot())
	}
	if _, ok := i.ResolvedAt(); ok {
		t.Error("resolved at kept after reopening")
	}
	if err := i.AttachTask("task-2", soon); err != nil || len(i.TaskIDs()) != 2 || i.CurrentTaskID() != "task-2" {
		t.Fatalf("attach in cycle 2: %v", err)
	}
	if err := i.Escalate("the triage task failed", soon); err != nil || i.Status() != IncidentEscalated || i.Reason() == "" {
		t.Fatalf("escalate: %v", err)
	}
	if err := i.Diagnose("late", soon); !errors.As(err, &serr) {
		t.Errorf("diagnose after escalation: %v", err)
	}
	if err := i.Resolve("", soon); err != nil || i.Reason() != "" {
		t.Fatalf("resolve an escalated incident: %v", err)
	}
}

func TestIncidentValidation(t *testing.T) {
	bad := map[string]func() error{
		"zero time": func() error {
			_, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "t", Sev3, nil, time.Time{})
			return err
		},
		"no tenant": func() error {
			_, err := NewIncident("inc-1", "", "triage", "g", "manual", "t", Sev3, nil, t0)
			return err
		},
		"empty group": func() error {
			_, err := NewIncident("inc-1", "ops", "triage", " ", "manual", "t", Sev3, nil, t0)
			return err
		},
		"bad source": func() error {
			_, err := NewIncident("inc-1", "ops", "triage", "g", "Alert Manager", "t", Sev3, nil, t0)
			return err
		},
		"empty title": func() error {
			_, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "", Sev3, nil, t0)
			return err
		},
		"bad severity": func() error {
			_, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "t", "sev9", nil, t0)
			return err
		},
		"empty alert": func() error {
			_, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "t", Sev3, []string{" "}, t0)
			return err
		},
		"bad diagnosis": func() error { i := sampleIncident(t); return i.Diagnose(" ", t0) },
		"bad reason":    func() error { i := sampleIncident(t); return i.Escalate("", t0) },
		"empty task":    func() error { i := sampleIncident(t); return i.AttachTask("", t0) },
	}
	var verr ValidationError
	for name, attempt := range bad {
		if err := attempt(); !errors.As(err, &verr) {
			t.Errorf("%s: %v", name, err)
		}
	}
	many := make([]string, maxIncidentFingerprints+1)
	for n := range many {
		many[n] = "fp-" + string(rune('a'+n%26)) + string(rune('a'+n/26))
	}
	if _, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "t", Sev3, many, t0); err == nil {
		t.Error("65 alerts accepted")
	}
	if severity, err := ParseIncidentSeverity(""); err != nil || severity != Sev3 {
		t.Errorf("default severity = %q %v", severity, err)
	}
	if _, err := ParseIncidentStatus("Closed"); err == nil {
		t.Error("unknown status parsed")
	}
}

func TestIncidentSnapshotRoundTrip(t *testing.T) {
	i := sampleIncident(t)
	_ = i.AttachTask("task-1", t0)
	_ = i.Diagnose("OOMKilled: limit 32Mi", t0)
	_ = i.AddCost(0.01, t0)
	_ = i.Resolve("green", t0.Add(time.Minute))
	snapshot := i.Snapshot()
	back, err := RestoreIncident(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if again := back.Snapshot(); again.ID != snapshot.ID || again.Status != "Resolved" || again.Diagnosis != snapshot.Diagnosis || again.CostUSD != 0.01 ||
		len(again.TaskIDs) != 1 || again.ResolvedAt == nil || !again.ResolvedAt.Equal(*snapshot.ResolvedAt) || len(again.Fingerprints) != 2 {
		t.Errorf("round trip differs: %+v", again)
	}
	broken := map[string]func(*IncidentSnapshot){
		"bad id":             func(s *IncidentSnapshot) { s.ID = "" },
		"bad tenant":         func(s *IncidentSnapshot) { s.TenantID = "" },
		"bad bot":            func(s *IncidentSnapshot) { s.BotID = "" },
		"bad severity":       func(s *IncidentSnapshot) { s.Severity = "high" },
		"bad status":         func(s *IncidentSnapshot) { s.Status = "Closed" },
		"zero updated":       func(s *IncidentSnapshot) { s.UpdatedAt = time.Time{} },
		"zero seen":          func(s *IncidentSnapshot) { s.LastSeenAt = time.Time{} },
		"zero cycle":         func(s *IncidentSnapshot) { s.Cycle = 0 },
		"too many tasks":     func(s *IncidentSnapshot) { s.TaskIDs = []string{"task-1", "task-2"} },
		"negative repeats":   func(s *IncidentSnapshot) { s.Repeats = -1 },
		"resolved without":   func(s *IncidentSnapshot) { s.ResolvedAt = nil },
		"open with resolved": func(s *IncidentSnapshot) { s.Status = "Open" },
		"escalated silent":   func(s *IncidentSnapshot) { s.Status = "Escalated"; s.ResolvedAt = nil; s.Reason = "" },
		"bad task id":        func(s *IncidentSnapshot) { s.TaskIDs = []string{"Task One"} },
		"long diagnosis":     func(s *IncidentSnapshot) { s.Diagnosis = string(make([]byte, maxIncidentDiagnosisLength+1)) },
		"long reason":        func(s *IncidentSnapshot) { s.Reason = string(make([]byte, maxIncidentReasonLength+1)) },
	}
	for name, change := range broken {
		s := i.Snapshot()
		change(&s)
		if _, err := RestoreIncident(s); err == nil {
			t.Errorf("%s: restored", name)
		}
	}
}

func TestTaskForIncidentAndIncidentEvents(t *testing.T) {
	task, _ := NewTask("task-1", "triage", "look", "k", "trace-1", t0)
	if err := task.ForIncident(""); err == nil {
		t.Error("empty incident linked")
	}
	if err := task.ForIncident("inc-1"); err != nil || task.IncidentID() != "inc-1" || task.Snapshot().IncidentID != "inc-1" {
		t.Fatalf("link: %v", err)
	}
	back, err := RestoreTask(task.Snapshot())
	if err != nil || back.IncidentID() != "inc-1" {
		t.Errorf("restore: %v %q", err, back.IncidentID())
	}
	broken := task.Snapshot()
	broken.IncidentID = "Inc One"
	if _, err := RestoreTask(broken); err == nil {
		t.Error("bad incident id restored")
	}
	event, err := NewEvent("evt-1", EventIncidentOpened, "ops", "triage", "task-1", "trace-1", t0, nil)
	if err == nil || event.ID != "" {
		t.Error("an incident event without an incident validated")
	}
	event = Event{ID: "evt-1", SchemaVersion: EventSchemaV1, Type: EventIncidentOpened, TenantID: "ops", BotID: "triage", IncidentID: "inc-1", CreatedAt: t0}
	if err := event.Validate(); err != nil || !EventIncidentOpened.AboutIncident() || EventTaskCompleted.AboutIncident() {
		t.Errorf("incident event: %v", err)
	}
	trigger, err := NewTrigger(TriggerSnapshot{Type: "incident", Objective: "Diagnose {{.title}}"})
	if err != nil || trigger.Type() != TriggerIncident || trigger.Name() != "incident" || trigger.ObjectiveTemplate() != "Diagnose {{.title}}" {
		t.Fatalf("incident trigger: %v", err)
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "incident", Every: "1m"}); err == nil {
		t.Error("an incident trigger with a schedule validated")
	}
}
