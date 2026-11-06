package domain

import (
	"errors"
	"strconv"
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
	if err := i.AddCost(Usage{CostUSD: 0.25}, t1); err != nil || i.CostUSD() != 0.25 || i.CostUnknown() {
		t.Fatalf("cost: %v", err)
	}
	if err := i.AddCost(Usage{CostUSD: -1}, t1); err == nil {
		t.Error("negative cost added")
	}
	// A run stopped before it said its cost leaves the sum a floor.
	if err := i.AddCost(UnknownUsage(0), t1); err != nil || i.CostUSD() != 0.25 || !i.CostUnknown() {
		t.Fatalf("unknown cost: %v %v", err, i.CostUnknown())
	}
	if _ = i.AddCost(Usage{CostUSD: 0.5}, t1); !i.CostUnknown() {
		t.Error("a known cost after an unknown one cleared the marker")
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
		"fix by nobody": func() error {
			i := sampleIncident(t)
			_ = i.Diagnose("x", t0)
			return i.AttachFixTask("task-2", "", t0)
		},
		"bad proposal": func() error {
			i := sampleIncident(t)
			_ = i.Diagnose("x", t0)
			_ = i.AttachFixTask("task-2", "fix", t0)
			return i.Propose(" ", t0)
		},
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
	if big, err := NewIncident("inc-1", "ops", "triage", "g", "manual", "t", Sev3, many, t0); err != nil || len(big.Fingerprints()) != maxIncidentFingerprints {
		t.Errorf("a big group: %v, %d alerts kept", err, len(big.Fingerprints()))
	}
	if severity, err := ParseIncidentSeverity(""); err != nil || severity != Sev3 {
		t.Errorf("default severity = %q %v", severity, err)
	}
	if _, err := ParseIncidentStatus("Closed"); err == nil {
		t.Error("unknown status parsed")
	}
}

// An Escalated incident takes IncidentEscalationRetries more triages; a
// proposal gives it its retries back, and so does a reopening, but not a
// diagnosis: a fix that fails every time would retry for ever.
func TestIncidentRetriesWhileEscalated(t *testing.T) {
	i := sampleIncident(t)
	_ = i.AttachTask("task-1", t0)
	if i.CanRetry() || i.Retry([]string{"fp-a"}, t0) == nil {
		t.Fatal("an Open incident retried")
	}
	for n := 1; n <= IncidentEscalationRetries; n++ {
		if err := i.Escalate("task failed", t0); err != nil {
			t.Fatal(err)
		}
		if !i.CanRetry() {
			t.Fatalf("retry %d refused", n)
		}
		if err := i.Retry([]string{"fp-c"}, t0.Add(time.Duration(n)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if i.Status() != IncidentOpen || i.Cycle() != n+1 || i.Retries() != n || i.Reason() != "task failed" {
			t.Fatalf("retry %d: %+v", n, i.Snapshot())
		}
		_ = i.AttachTask(TaskID("task-r"+string(rune('0'+n))), t0)
	}
	_ = i.Escalate("task failed", t0)
	if i.CanRetry() || i.Retry(nil, t0) == nil {
		t.Fatal("retried past the limit")
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || back.Retries() != IncidentEscalationRetries || back.CanRetry() {
		t.Fatalf("restored %+v %v", back.Snapshot(), err)
	}
	_ = i.Resolve("green", t0.Add(time.Hour))
	if err := i.Reopen([]string{"fp-a"}, t0.Add(time.Hour+time.Minute)); err != nil || i.Retries() != 0 {
		t.Fatalf("reopened retries %d %v", i.Retries(), err)
	}
	_ = i.AttachTask("task-x", t0)
	_ = i.Escalate("again", t0)
	_ = i.Retry(nil, t0)
	_ = i.AttachTask("task-y", t0)
	if err := i.Diagnose("found it", t0); err != nil || i.Retries() != 1 {
		t.Fatalf("diagnosed retries %d %v", i.Retries(), err)
	}
	_ = i.AttachFixTask("task-z", "fix", t0)
	if err := i.Propose("rollout undo", t0); err != nil || i.Retries() != 0 {
		t.Fatalf("proposed retries %d %v", i.Retries(), err)
	}
}

func TestIncidentSnapshotRoundTrip(t *testing.T) {
	i := sampleIncident(t)
	_ = i.AttachTask("task-1", t0)
	_ = i.Diagnose("OOMKilled: limit 32Mi", t0)
	_ = i.AddCost(Usage{CostUSD: 0.01, CostUnknown: true}, t0)
	_ = i.Resolve("green", t0.Add(time.Minute))
	snapshot := i.Snapshot()
	back, err := RestoreIncident(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if again := back.Snapshot(); again.ID != snapshot.ID || again.Status != "Resolved" || again.Diagnosis != snapshot.Diagnosis || again.CostUSD != 0.01 || !again.CostUnknown ||
		len(again.TaskIDs) != 1 || again.ResolvedAt == nil || !again.ResolvedAt.Equal(*snapshot.ResolvedAt) || len(again.Fingerprints) != 2 {
		t.Errorf("round trip differs: %+v", again)
	}
	broken := map[string]func(*IncidentSnapshot){
		"bad id":       func(s *IncidentSnapshot) { s.ID = "" },
		"bad tenant":   func(s *IncidentSnapshot) { s.TenantID = "" },
		"bad bot":      func(s *IncidentSnapshot) { s.BotID = "" },
		"bad severity": func(s *IncidentSnapshot) { s.Severity = "high" },
		"bad status":   func(s *IncidentSnapshot) { s.Status = "Closed" },
		"zero updated": func(s *IncidentSnapshot) { s.UpdatedAt = time.Time{} },
		"zero seen":    func(s *IncidentSnapshot) { s.LastSeenAt = time.Time{} },
		"zero cycle":   func(s *IncidentSnapshot) { s.Cycle = 0 },
		"too many tasks": func(s *IncidentSnapshot) {
			s.Tasks = append(s.Tasks, IncidentTaskSnapshot{TaskID: "task-2", BotID: "triage", Phase: "triage", Cycle: 1})
		},
		"task past cycle": func(s *IncidentSnapshot) { s.Tasks[0].Cycle = 2 },
		"task out of order": func(s *IncidentSnapshot) {
			s.Cycle = 2
			s.Tasks = []IncidentTaskSnapshot{{"task-2", "triage", "triage", 2}, {"task-1", "triage", "triage", 1}}
		},
		"bad task phase":     func(s *IncidentSnapshot) { s.Tasks[0].Phase = "verify" },
		"bad task bot":       func(s *IncidentSnapshot) { s.Tasks[0].BotID = "" },
		"legacy too many":    func(s *IncidentSnapshot) { s.Tasks = nil; s.TaskIDs = []string{"task-1", "task-2"} },
		"legacy bad task id": func(s *IncidentSnapshot) { s.Tasks = nil; s.TaskIDs = []string{"Task One"} },
		"proposed silent":    func(s *IncidentSnapshot) { s.Status = "Proposed"; s.ResolvedAt = nil; s.Proposal = "" },
		"long proposal":      func(s *IncidentSnapshot) { s.Proposal = string(make([]byte, MaxIncidentProposalLength+1)) },
		"negative repeats":   func(s *IncidentSnapshot) { s.Repeats = -1 },
		"negative retries":   func(s *IncidentSnapshot) { s.Retries = -1 },
		"resolved without":   func(s *IncidentSnapshot) { s.ResolvedAt = nil },
		"open with resolved": func(s *IncidentSnapshot) { s.Status = "Open" },
		"escalated silent":   func(s *IncidentSnapshot) { s.Status = "Escalated"; s.ResolvedAt = nil; s.Reason = "" },
		"bad task id":        func(s *IncidentSnapshot) { s.Tasks[0].TaskID = "Task One" },
		"long diagnosis":     func(s *IncidentSnapshot) { s.Diagnosis = string(make([]byte, MaxIncidentDiagnosisLength+1)) },
		"long reason":        func(s *IncidentSnapshot) { s.Reason = string(make([]byte, MaxIncidentReasonLength+1)) },
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

func TestIncidentResolvesOnlyTheAlertsItKept(t *testing.T) {
	fingerprints := make([]string, 70)
	for n := range fingerprints {
		fingerprints[n] = "fp-" + strconv.Itoa(n)
	}
	i, err := NewIncident("inc-big", "ops", "triage", "g", "alertmanager", "a node down", Sev2, fingerprints, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := i.MarkResolved(fingerprints, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(back.Fingerprints()) != maxIncidentFingerprints || len(back.ResolvedFingerprints()) != maxIncidentFingerprints || len(back.FiringFingerprints()) != 0 {
		t.Errorf("seen %d, resolved %d, firing %d", len(back.Fingerprints()), len(back.ResolvedFingerprints()), len(back.FiringFingerprints()))
	}
}

// Diagnosed → Proposed: the fix task of the cycle is attached once, by
// its own bot, and its answer is the proposal; a new diagnosis drops it.
func TestIncidentFixPhase(t *testing.T) {
	i := sampleIncident(t)
	var serr StateError
	if err := i.AttachFixTask("task-f", "fix", t0); !errors.As(err, &serr) {
		t.Errorf("fix task while Open: %v", err)
	}
	_ = i.AttachTask("task-1", t0)
	if err := i.Propose("too soon", t0); !errors.As(err, &serr) {
		t.Errorf("propose while Open: %v", err)
	}
	_ = i.Diagnose("drift: replicas 1, desired 3", t0)
	if err := i.Propose("no fix task", t0); !errors.As(err, &serr) {
		t.Errorf("propose without a fix task: %v", err)
	}
	if i.HasTask(IncidentPhaseFix) || !i.HasTask(IncidentPhaseTriage) {
		t.Fatal("phases before the fix task")
	}
	t1 := t0.Add(time.Minute)
	if err := i.AttachFixTask("task-f", "fix", t1); err != nil || i.CurrentTaskID() != "task-f" || !i.HasTask(IncidentPhaseFix) {
		t.Fatalf("attach fix: %v", err)
	}
	if current, _ := i.CurrentTask(); current.BotID() != "fix" || current.Phase() != IncidentPhaseFix || current.Cycle() != 1 {
		t.Errorf("current task = %+v", current.Snapshot())
	}
	if err := i.AttachFixTask("task-g", "fix", t1); !errors.As(err, &serr) {
		t.Errorf("second fix task: %v", err)
	}
	if err := i.Propose("PROPOSAL: kubectl apply -f desired/web.yaml\nROLLBACK: replicas 1", t1); err != nil || i.Status() != IncidentProposed || i.Proposal() == "" {
		t.Fatalf("propose: %v", err)
	}
	if err := i.Propose("twice", t1); !errors.As(err, &serr) {
		t.Errorf("second proposal: %v", err)
	}
	if err := i.Escalate("why", t1); !errors.As(err, &serr) {
		t.Errorf("escalate a proposal: %v", err)
	}
	if err := i.Repeat([]string{"fp-a"}, t1); err != nil || i.Status() != IncidentProposed {
		t.Errorf("repeat while Proposed: %v", err)
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || back.Status() != IncidentProposed || back.Proposal() != i.Proposal() || len(back.Tasks()) != 2 || back.Tasks()[1].BotID() != "fix" {
		t.Fatalf("round trip: %+v %v", back.Snapshot(), err)
	}
	if ids := back.Snapshot().TaskIDs; len(ids) != 2 || ids[0] != "task-1" || ids[1] != "task-f" {
		t.Errorf("task ids = %v", ids)
	}
	// Resolved and reopened: the proposal stays until a new diagnosis.
	_ = i.Resolve("green", t1)
	_ = i.Reopen([]string{"fp-a"}, t1.Add(time.Minute))
	_ = i.AttachTask("task-2", t1)
	if i.Proposal() == "" || i.HasTask(IncidentPhaseFix) {
		t.Fatalf("reopened: %+v", i.Snapshot())
	}
	_ = i.Diagnose("a new cause", t1)
	if i.Proposal() != "" {
		t.Error("a new diagnosis kept the old proposal")
	}
	// A failed fix task escalates from Diagnosed.
	_ = i.AttachFixTask("task-f2", "fix", t1)
	if err := i.Escalate("fix task task-f2 Failed: kube-mcp down", t1); err != nil || i.Status() != IncidentEscalated || i.Diagnosis() != "a new cause" {
		t.Fatalf("escalate a failed fix: %v", err)
	}
}

// An incident stored before tasks had phases has only taskIds: its
// triage tasks, one per cycle, run by the incident's bot.
func TestIncidentRestoresTaskIDsAsTriage(t *testing.T) {
	i := sampleIncident(t)
	_ = i.AttachTask("task-1", t0)
	_ = i.Diagnose("found it", t0)
	s := i.Snapshot()
	s.Tasks = nil
	back, err := RestoreIncident(s)
	if err != nil {
		t.Fatal(err)
	}
	if tasks := back.Tasks(); len(tasks) != 1 || tasks[0].TaskID() != "task-1" || tasks[0].BotID() != "triage" || tasks[0].Phase() != IncidentPhaseTriage || tasks[0].Cycle() != 1 {
		t.Errorf("tasks = %+v", back.Snapshot().Tasks)
	}
	if !back.HasTask(IncidentPhaseTriage) || back.HasTask(IncidentPhaseFix) {
		t.Error("phases of a stored incident")
	}
}

func TestIncidentPhase(t *testing.T) {
	for raw, want := range map[string]IncidentPhase{"": IncidentPhaseTriage, "triage": IncidentPhaseTriage, "fix": IncidentPhaseFix} {
		if got, err := ParseIncidentPhase(raw); err != nil || got != want {
			t.Errorf("%q = %q %v", raw, got, err)
		}
	}
	if _, err := ParseIncidentPhase("verify"); err == nil {
		t.Error("an unknown phase parsed")
	}
	if _, err := NewIncidentTask("task-1", "fix", "", 1); err == nil {
		t.Error("a task without a phase")
	}
	if _, err := NewIncidentTask("task-1", "fix", IncidentPhaseFix, 0); err == nil {
		t.Error("a task in cycle 0")
	}
	fix, err := NewTrigger(TriggerSnapshot{Type: "incident", Name: "fix-phase", Phase: "fix", Objective: "Fix {{.diagnosis}}"})
	if err != nil || fix.Phase() != IncidentPhaseFix || fix.Snapshot().Phase != "fix" {
		t.Fatalf("fix trigger: %v", err)
	}
	triage, _ := NewTrigger(TriggerSnapshot{Type: "incident"})
	if triage.Phase() != IncidentPhaseTriage || triage.Snapshot().Phase != "" {
		t.Errorf("triage trigger phase %q, stored %q", triage.Phase(), triage.Snapshot().Phase)
	}
	for _, bad := range []TriggerSnapshot{{Type: "incident", Phase: "verify"}, {Type: "manual", Phase: "fix"}} {
		if _, err := NewTrigger(bad); err == nil {
			t.Errorf("%+v validated", bad)
		}
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "manual"}, {Type: "incident", Name: "fix", Phase: "fix"}})
	if found, ok := triggers.Incident(IncidentPhaseFix); !ok || found.Name() != "fix" {
		t.Error("the fix trigger was not found")
	}
	if _, ok := triggers.Incident(IncidentPhaseTriage); ok {
		t.Error("a fix trigger took triage")
	}
	if !EventIncidentProposed.AboutIncident() {
		t.Error("incident.proposed is not about an incident")
	}
	if got, err := ParseEventType("incident.proposed"); err != nil || got != EventIncidentProposed {
		t.Errorf("incident.proposed = %q %v", got, err)
	}
}
