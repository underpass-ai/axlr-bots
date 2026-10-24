package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

func sampleBot(t *testing.T) Bot {
	t.Helper()
	id, _ := NewBotID("server-incident-investigator")
	tenant, _ := NewTenantID("engineering")
	name, _ := NewBotName("Server Incident Investigator")
	owner, _ := NewOwner("platform-team")
	mission, _ := NewMission("Diagnose server and application incidents")
	tools, _ := NewAllowedTools([]string{"observability.query_logs", "kubernetes.get_pods"})
	model, _ := NewModelPolicy("anthropic/claude-haiku-5.5", "cost-optimized", false, 32000)
	budgets, _ := NewBudgets(20, 60, 2, 900*time.Second)
	bot, err := NewBot(id, tenant, name, owner, mission, tools, model, budgets, t0)
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

func TestBotLifecycle(t *testing.T) {
	bot := sampleBot(t)
	if bot.Status() != BotDraft || bot.AcceptsTasks() {
		t.Fatal("new bot should be Draft and accept no tasks")
	}
	if bot.ID() != "server-incident-investigator" || bot.TenantID() != "engineering" || bot.Name() != "Server Incident Investigator" ||
		bot.Owner() != "platform-team" || bot.Mission() == "" || bot.AllowedTools().Len() != 2 || bot.ModelPolicy().Model() == "" ||
		bot.Budgets().TaskTimeout() != 900*time.Second || !bot.CreatedAt().Equal(t0) || !bot.UpdatedAt().Equal(t0) {
		t.Error("getters wrong")
	}
	if err := bot.Enable(t0); err == nil {
		t.Error("Draft bot enabled")
	}
	if err := bot.Pause(t0); err == nil {
		t.Error("Draft bot paused")
	}
	t1 := t0.Add(time.Minute)
	if err := bot.Register(t1); err != nil || bot.Status() != BotRegistered || !bot.UpdatedAt().Equal(t1) {
		t.Fatalf("register: %v %s", err, bot.Status())
	}
	var serr StateError
	if err := bot.Register(t1); !errors.As(err, &serr) {
		t.Errorf("second register: %v", err)
	}
	if err := bot.Enable(t1); err != nil || !bot.AcceptsTasks() {
		t.Fatalf("enable: %v", err)
	}
	if err := bot.Pause(t1); err != nil || bot.Status() != BotPaused {
		t.Fatalf("pause: %v", err)
	}
	if err := bot.Enable(t1); err != nil || bot.Status() != BotEnabled {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := NewBot(bot.id, bot.tenantID, bot.name, bot.owner, bot.mission, bot.allowedTools, bot.modelPolicy, bot.budgets, time.Time{}); err == nil {
		t.Error("zero time accepted")
	}
}

func TestBotSnapshotRoundTrip(t *testing.T) {
	bot := sampleBot(t)
	_ = bot.Register(t0)
	_ = bot.Enable(t0.Add(time.Second))
	snap := bot.Snapshot()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var back BotSnapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreBot(back)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status() != BotEnabled || restored.ID() != bot.ID() || restored.AllowedTools().Len() != 2 ||
		restored.Budgets().TaskTimeout() != 900*time.Second || restored.ModelPolicy().MaxContextTokens() != 32000 ||
		!restored.UpdatedAt().Equal(bot.UpdatedAt()) {
		t.Errorf("restored differs: %+v", restored.Snapshot())
	}
	bad := []func(*BotSnapshot){
		func(s *BotSnapshot) { s.ID = "" },
		func(s *BotSnapshot) { s.TenantID = "" },
		func(s *BotSnapshot) { s.Name = "" },
		func(s *BotSnapshot) { s.Owner = "" },
		func(s *BotSnapshot) { s.Mission = "" },
		func(s *BotSnapshot) { s.AllowedTools = []string{"Bad"} },
		func(s *BotSnapshot) { s.Model.RoutingPolicy = "x" },
		func(s *BotSnapshot) { s.Budgets.MaxToolCallsPerTask = -1 },
		func(s *BotSnapshot) { s.Status = "Gone" },
		func(s *BotSnapshot) { s.CreatedAt = time.Time{} },
		func(s *BotSnapshot) { s.UpdatedAt = time.Time{} },
	}
	for i, mutate := range bad {
		s := bot.Snapshot()
		mutate(&s)
		if _, err := RestoreBot(s); err == nil {
			t.Errorf("bad snapshot %d restored", i)
		}
	}
}

func sampleTask(t *testing.T) Task {
	t.Helper()
	id, _ := NewTaskID("task-1")
	bot, _ := NewBotID("bot-1")
	objective, _ := NewObjective("Investigate an HTTP 500 spike")
	key, _ := NewIdempotencyKey("alert-1")
	corr, _ := NewCorrelationID("trace-abc")
	task, err := NewTask(id, bot, objective, key, corr, t0)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTaskLifecycle(t *testing.T) {
	task := sampleTask(t)
	if task.Status() != TaskPending || task.CurrentExecutionID() != "" || len(task.ExecutionIDs()) != 0 {
		t.Fatal("new task state wrong")
	}
	if task.ID() != "task-1" || task.BotID() != "bot-1" || task.Objective() == "" || task.IdempotencyKey() != "alert-1" ||
		task.CorrelationID() != "trace-abc" || task.FailureReason() != "" || !task.CreatedAt().Equal(t0) || !task.UpdatedAt().Equal(t0) {
		t.Error("getters wrong")
	}
	for _, attempt := range []func() error{
		func() error { return task.Complete(t0) },
		func() error { return task.Fail("x", t0) },
		func() error { return task.AwaitApproval(t0) },
		func() error { return task.Resume("exec-9", t0) },
	} {
		if err := attempt(); err == nil {
			t.Error("transition from Pending accepted")
		}
	}
	if err := task.Start("", t0); err == nil {
		t.Error("empty execution id accepted")
	}
	t1 := t0.Add(time.Second)
	if err := task.Start("exec-1", t1); err != nil || task.Status() != TaskRunning || task.CurrentExecutionID() != "exec-1" {
		t.Fatalf("start: %v", err)
	}
	if err := task.Start("exec-2", t1); err == nil {
		t.Error("second start accepted: a Running task must not run again")
	}
	if err := task.Fail("", t1); err == nil {
		t.Error("empty failure reason accepted")
	}
	if err := task.AwaitApproval(t1); err != nil || task.Status() != TaskWaitingApproval {
		t.Fatalf("await: %v", err)
	}
	if err := task.Resume("", t1); err == nil {
		t.Error("resume with an empty execution id accepted")
	}
	if err := task.Resume("exec-2", t1); err != nil || task.Status() != TaskRunning || task.CurrentExecutionID() != "exec-2" || len(task.ExecutionIDs()) != 2 {
		t.Fatalf("resume: %v", err)
	}
	if err := task.Complete(t1); err != nil || task.Status() != TaskCompleted || !task.UpdatedAt().Equal(t1) {
		t.Fatalf("complete: %v", err)
	}
	if err := task.Start("exec-3", t1); err == nil {
		t.Error("Completed task started again")
	}
	if err := task.Cancel(t1); err == nil {
		t.Error("Completed task cancelled")
	}

	failing := sampleTask(t)
	_ = failing.Start("exec-1", t1)
	if err := failing.Fail("runtime exited 1", t1); err != nil || failing.Status() != TaskFailed || failing.FailureReason() != "runtime exited 1" {
		t.Fatalf("fail: %v", err)
	}
	cancelled := sampleTask(t)
	if err := cancelled.Cancel(t1); err != nil || cancelled.Status() != TaskCancelled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := NewTask("t", "b", "o", "k", "", time.Time{}); err == nil {
		t.Error("zero time accepted")
	}
}

func TestTaskSnapshotRoundTrip(t *testing.T) {
	task := sampleTask(t)
	_ = task.Start("exec-1", t0)
	_ = task.Fail("boom", t0.Add(time.Second))
	raw, _ := json.Marshal(task.Snapshot())
	var snap TaskSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreTask(snap)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status() != TaskFailed || restored.FailureReason() != "boom" || restored.CurrentExecutionID() != "exec-1" || restored.CorrelationID() != "trace-abc" {
		t.Errorf("restored differs: %+v", restored.Snapshot())
	}
	bad := []func(*TaskSnapshot){
		func(s *TaskSnapshot) { s.ID = "" },
		func(s *TaskSnapshot) { s.BotID = "" },
		func(s *TaskSnapshot) { s.Objective = "" },
		func(s *TaskSnapshot) { s.IdempotencyKey = "" },
		func(s *TaskSnapshot) { s.CorrelationID = "has space" },
		func(s *TaskSnapshot) { s.Status = "Odd" },
		func(s *TaskSnapshot) { s.CreatedAt = time.Time{} },
		func(s *TaskSnapshot) { s.UpdatedAt = time.Time{} },
		func(s *TaskSnapshot) { s.ExecutionIDs = []string{"Bad Id"} },
		func(s *TaskSnapshot) { s.FailureReason = "" },
	}
	for i, mutate := range bad {
		s := task.Snapshot()
		mutate(&s)
		if _, err := RestoreTask(s); err == nil {
			t.Errorf("bad snapshot %d restored", i)
		}
	}
}

func sampleExecution(t *testing.T) Execution {
	t.Helper()
	e, err := NewExecution("exec-1", "task-1", "bot-1", t0)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExecutionLifecycle(t *testing.T) {
	e := sampleExecution(t)
	if e.Outcome() != ExecutionRunning || !e.EndedAt().IsZero() || e.ID() != "exec-1" || e.TaskID() != "task-1" || e.BotID() != "bot-1" || !e.StartedAt().Equal(t0) {
		t.Fatal("new execution wrong")
	}
	if _, err := NewExecution("e", "t", "b", time.Time{}); err == nil {
		t.Error("zero start accepted")
	}
	result := Result{Answer: "The spike came from pod api-3", SessionID: "sess-1", ToolCalls: []ToolCall{{Name: "local_read", CallID: "c1", Decision: "approve", Status: "completed"}}}
	usage := Usage{ToolCalls: 1}
	t1 := t0.Add(2 * time.Second)
	if err := e.Complete(result, usage, "/traces/exec-1.jsonl", t0.Add(-time.Second)); err == nil {
		t.Error("ended before started accepted")
	}
	if err := e.Complete(result, Usage{Requests: -1}, "", t1); err == nil {
		t.Error("negative usage accepted")
	}
	if err := e.Complete(result, usage, "/traces/exec-1.jsonl", t1); err != nil {
		t.Fatal(err)
	}
	if e.Outcome() != ExecutionCompleted || e.Result().Answer != result.Answer || e.Usage().ToolCalls != 1 || e.TracePath() != "/traces/exec-1.jsonl" || !e.EndedAt().Equal(t1) || e.FailureReason() != "" {
		t.Errorf("completed execution wrong: %+v", e.Snapshot())
	}
	if err := e.Fail("late", Result{}, Usage{}, "", t1); err == nil {
		t.Error("finished execution ended again")
	}

	failed := sampleExecution(t)
	if err := failed.Fail("", Result{}, Usage{}, "", t1); err == nil {
		t.Error("empty reason accepted")
	}
	if err := failed.Fail("timed out after 1s", Result{Answer: "partial"}, Usage{}, "/t", t1); err != nil || failed.Outcome() != ExecutionFailed || failed.FailureReason() != "timed out after 1s" || failed.Result().Answer != "partial" {
		t.Errorf("fail: %v", err)
	}

	waiting := sampleExecution(t)
	if err := waiting.RequireDecision(Result{}, Usage{}, "", t1); err == nil {
		t.Error("decision required without a pending decision accepted")
	}
	pending := &PendingDecision{Tool: "local_exec", CallID: "c9", SessionID: "sess-2"}
	if err := waiting.RequireDecision(Result{SessionID: "sess-2", PendingDecision: pending}, Usage{}, "", t1); err != nil || waiting.Outcome() != ExecutionDecisionRequired || waiting.Result().PendingDecision.CallID != "c9" {
		t.Errorf("require decision: %v", err)
	}
}

func TestExecutionSnapshotRoundTrip(t *testing.T) {
	running := sampleExecution(t)
	snap := running.Snapshot()
	if snap.EndedAt != nil {
		t.Error("running execution has an end")
	}
	if back, err := RestoreExecution(snap); err != nil || back.Outcome() != ExecutionRunning {
		t.Errorf("restore running: %v", err)
	}
	for _, finish := range []func(*Execution){
		func(e *Execution) { _ = e.Complete(Result{Answer: "a"}, Usage{Requests: 2}, "/t", t0.Add(time.Second)) },
		func(e *Execution) { _ = e.Fail("why", Result{}, Usage{}, "", t0.Add(time.Second)) },
		func(e *Execution) {
			_ = e.RequireDecision(Result{PendingDecision: &PendingDecision{Tool: "x", CallID: "c"}}, Usage{}, "", t0.Add(time.Second))
		},
	} {
		e := sampleExecution(t)
		finish(&e)
		raw, _ := json.Marshal(e.Snapshot())
		var s ExecutionSnapshot
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		back, err := RestoreExecution(s)
		if err != nil {
			t.Fatalf("%s: %v", e.Outcome(), err)
		}
		if back.Outcome() != e.Outcome() || back.Result().Answer != e.Result().Answer || back.Usage() != e.Usage() || !back.EndedAt().Equal(e.EndedAt()) {
			t.Errorf("%s restored differently", e.Outcome())
		}
	}
	done := sampleExecution(t)
	_ = done.Complete(Result{}, Usage{}, "", t0)
	bad := []func(*ExecutionSnapshot){
		func(s *ExecutionSnapshot) { s.ID = "" },
		func(s *ExecutionSnapshot) { s.TaskID = "" },
		func(s *ExecutionSnapshot) { s.BotID = "" },
		func(s *ExecutionSnapshot) { s.Outcome = "Odd" },
		func(s *ExecutionSnapshot) { s.StartedAt = time.Time{} },
		func(s *ExecutionSnapshot) { s.EndedAt = nil },
		func(s *ExecutionSnapshot) { s.Outcome = "Failed" },
		func(s *ExecutionSnapshot) { s.Outcome = "DecisionRequired" },
	}
	for i, mutate := range bad {
		s := done.Snapshot()
		mutate(&s)
		if _, err := RestoreExecution(s); err == nil {
			t.Errorf("bad snapshot %d restored", i)
		}
	}
}

func TestEvent(t *testing.T) {
	payload := map[string]any{"objective": "Investigate an HTTP 500 spike", "readOnly": true}
	event, err := NewEvent("evt-1", EventTaskSubmitted, "underpass", "bot-1", "task-1", "trace-abc", t0, payload)
	if err != nil {
		t.Fatal(err)
	}
	if event.SchemaVersion != EventSchemaV1 || event.Type != EventTaskSubmitted || event.Payload["readOnly"] != true {
		t.Errorf("event wrong: %+v", event)
	}
	raw, _ := json.Marshal(event)
	for _, want := range []string{`"eventId":"evt-1"`, `"schemaVersion":"underpass.ai/events/v1"`, `"type":"bot.task.submitted"`, `"taskId":"task-1"`} {
		if !contains(string(raw), want) {
			t.Errorf("json lacks %s: %s", want, raw)
		}
	}
	bad := []Event{
		{SchemaVersion: EventSchemaV1, Type: EventBotEnabled, TenantID: "t", BotID: "b", CreatedAt: t0},
		{ID: "e", Type: EventBotEnabled, TenantID: "t", BotID: "b", CreatedAt: t0},
		{ID: "e", SchemaVersion: EventSchemaV1, Type: EventBotEnabled, BotID: "b", CreatedAt: t0},
		{ID: "e", SchemaVersion: EventSchemaV1, Type: EventBotEnabled, TenantID: "t", CreatedAt: t0},
		{ID: "e", SchemaVersion: EventSchemaV1, Type: EventBotEnabled, TenantID: "t", BotID: "b"},
		{ID: "e", SchemaVersion: EventSchemaV1, Type: "bot.unknown", TenantID: "t", BotID: "b", CreatedAt: t0},
	}
	for i, e := range bad {
		if err := e.Validate(); err == nil {
			t.Errorf("bad event %d validated", i)
		}
	}
	if _, err := NewEvent("", EventBotEnabled, "t", "b", "", "", t0, nil); err == nil {
		t.Error("NewEvent accepted an empty id")
	}
}

func TestAuditEntry(t *testing.T) {
	entry, err := NewAuditEntry("aud-1", t0, "cli:gx10a", "task.run", "task-1", AuditOK, map[string]string{"execution": "exec-1"})
	if err != nil || entry.Details["execution"] != "exec-1" {
		t.Fatalf("%v", err)
	}
	cases := []struct {
		id, actor, action, subject string
		at                         time.Time
		outcome                    AuditOutcome
	}{
		{"", "a", "b", "c", t0, AuditOK},
		{"i", "a", "b", "c", time.Time{}, AuditOK},
		{"i", "", "b", "c", t0, AuditOK},
		{"i", "a", "", "c", t0, AuditOK},
		{"i", "a", "b", "", t0, AuditOK},
		{"i", "a", "b", "c", t0, "shrug"},
	}
	for i, c := range cases {
		if _, err := NewAuditEntry(c.id, c.at, c.actor, c.action, c.subject, c.outcome, nil); err == nil {
			t.Errorf("bad audit entry %d accepted", i)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
