package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func decideWatch(t *testing.T, h *harness, execution domain.ExecutionID, decision WatchDecision) WatchDecided {
	t.Helper()
	decided, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", execution, decision)
	if err != nil {
		t.Fatal(err)
	}
	return decided
}

// Jev chooses abandon: the question's task is cancelled with Jev's verdict
// as the reason, the asker, still running, hears answered false naming
// the watcher, and the audit says question.abandon. No event of its own.
func TestAWatcherAbandonsAStuckQuestionOfItsTeam(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(6 * time.Minute)
	next := watchNext(t, h, watcher.ExecutionID, "")
	decided := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: next.Cursor, Choice: "abandon", Probability: 0.83, Confidence: 0.71})
	reason := "abandoned by qa-vigia: Jev chose abandon (p=0.83, c=0.71)"
	if !decided.Abandoned || decided.Status != domain.TaskCancelled || decided.Reason != reason || decided.Question != q.TaskID || decided.Choice != "abandon" {
		t.Fatalf("decided = %+v", decided)
	}
	task := mustTask(t, h, q.TaskID)
	if task.Status() != domain.TaskCancelled || task.FailureReason() != reason {
		t.Errorf("the question: %s %q", task.Status(), task.FailureReason())
	}
	answered, ok := lastEvent(h, domain.EventQuestionAnswered)
	if !ok || answered.Payload["answered"] != false || answered.Payload["reason"] != reason {
		t.Fatalf("answered = %+v", answered.Payload)
	}
	if delivery, _ := answered.Payload["delivery"].(map[string]any); delivery["mode"] != "message" {
		t.Errorf("delivery = %+v", answered.Payload["delivery"])
	}
	message, ok := lastEvent(h, domain.EventTaskMessage)
	if !ok || message.TaskID != parent.Run.TaskID || !strings.Contains(message.Payload["text"].(string), "qa-vigia") {
		t.Errorf("the asker heard %+v", message.Payload)
	}
	cancelled, ok := lastEvent(h, domain.EventTaskCancelled)
	if !ok || cancelled.TaskID != q.TaskID || cancelled.Payload["watcher"] != "qa-vigia" || cancelled.Payload["probability"] != 0.83 {
		t.Errorf("cancelled = %+v", cancelled.Payload)
	}
	if !hasAudit(h, "question.abandon", domain.AuditOK) {
		t.Error("no question.abandon audit")
	}
	// It ended: nothing more to watch, and a late decision leaves it.
	if got := watchNext(t, h, watcher.ExecutionID, ""); !got.Done {
		t.Errorf("after the abandon = %+v", got)
	}
	if late := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "abandon"}); late.Abandoned || late.Status != domain.TaskCancelled {
		t.Errorf("late = %+v", late)
	}
}

// Jev chooses wait: nothing changes.
func TestWaitChangesNothing(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(6 * time.Minute)
	events, audits := len(h.log.events), len(h.log.audits)
	decided := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "wait", Probability: 0.9, Confidence: 0.8})
	if decided.Abandoned || decided.Status != domain.TaskPending || decided.Reason != "" {
		t.Errorf("decided = %+v", decided)
	}
	if status := mustTask(t, h, q.TaskID).Status(); status != domain.TaskPending || len(h.log.events) != events || len(h.log.audits) != audits {
		t.Errorf("wait changed something: %s, %d events, %d audits", status, len(h.log.events)-events, len(h.log.audits)-audits)
	}
}

// A watcher of another team never sees or touches the question: its round
// is done at once, and a decision on it is not found.
func TestAWatcherNeverTouchesAnotherTeamsQuestions(t *testing.T) {
	h, parent, watcher := watching(t, "dev")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(time.Hour)
	if got := watchNext(t, h, watcher.ExecutionID, ""); !got.Done || got.Team != "dev" {
		t.Errorf("dev's watcher saw %+v", got)
	}
	if _, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "abandon"}); !domain.IsNotFound(err) {
		t.Errorf("dev's watcher decided: %v", err)
	}
	// Nor a task that is no question.
	if _, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: parent.Run.TaskID, Choice: "wait"}); !domain.IsNotFound(err) {
		t.Errorf("a task that is no question: %v", err)
	}
	if status := mustTask(t, h, q.TaskID).Status(); status != domain.TaskPending {
		t.Errorf("the question is %s", status)
	}
}

// A choice that is neither wait nor abandon, or a verdict out of range,
// is refused before anything is read.
func TestADecisionIsValidated(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who?")
	h.clock.now = t0.Add(6 * time.Minute)
	for _, decision := range []WatchDecision{
		{Question: q.TaskID, Choice: "maybe"},
		{Question: q.TaskID, Choice: "abandon", Probability: 1.2},
		{Question: q.TaskID, Choice: "abandon", Confidence: -0.1},
	} {
		if _, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, decision); err == nil {
			t.Errorf("%+v accepted", decision)
		}
	}
	if _, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: "task-none", Choice: "wait"}); !domain.IsNotFound(err) {
		t.Errorf("an unknown question: %v", err)
	}
	if status := mustTask(t, h, q.TaskID).Status(); status != domain.TaskPending {
		t.Errorf("the question is %s", status)
	}
}

// The runner takes both under its lock.
func TestTheRunnerWatchesUnderItsLock(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who?")
	h.clock.now = t0.Add(6 * time.Minute)
	runner := NewSerialRunner(h.deps)
	next, err := runner.WatchNext(context.Background(), "jev-node", watcher.ExecutionID, "", false)
	if err != nil || next.Cursor != q.TaskID {
		t.Fatalf("next = %+v %v", next, err)
	}
	decided, err := runner.DecideWatch(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "abandon", Probability: 0.5, Confidence: 0.5})
	if err != nil || !decided.Abandoned {
		t.Errorf("decided = %+v %v", decided, err)
	}
}
