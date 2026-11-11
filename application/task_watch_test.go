package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func watchTasks(t *testing.T, h *harness, execution domain.ExecutionID, after domain.TaskID) WatchedQuestion {
	t.Helper()
	watched, err := NewWatchNext(h.deps).Execute(context.Background(), "jev-node", execution, after, true)
	if err != nil {
		t.Fatal(err)
	}
	return watched
}

// watchingWork is watching(t, "qa") with a qa teammate that takes over
// stuck work through a team subscription to warn.stuck.
func watchingWork(t *testing.T) (*harness, Assignment, Assignment) {
	t.Helper()
	h, parent, watcher := watching(t, "qa")
	channelBot(t, h, "qa-triaje", []string{"qa"}, nil, nil, "qa-node")
	subscribe(t, h, "qa-triaje", domain.SubscriptionSnapshot{Topic: "warn.stuck", Mode: "task", From: []string{"control-plane"}, Deliver: "team"})
	return h, parent, watcher
}

// A watcher whose node asks for tasks is put its team's own task that has
// run five minutes, with what the run reported; the watcher's own Jev task
// is never put, nor a task younger than five minutes, nor anything to an
// older node.
func TestAWatcherIsPutItsTeamsLongRunningTask(t *testing.T) {
	h, parent, watcher := watchingWork(t)
	h.clock.now = t0.Add(4 * time.Minute)
	if young := watchTasks(t, h, watcher.ExecutionID, ""); !young.Done {
		t.Fatalf("a task of 4 minutes was put: %+v", young)
	}
	h.clock.now = t0.Add(6 * time.Minute)
	if old := watchNext(t, h, watcher.ExecutionID, ""); !old.Done {
		t.Errorf("an older node was put a task: %+v", old)
	}
	next := watchTasks(t, h, watcher.ExecutionID, "")
	if next.Done || next.Task == nil || next.Cursor != parent.Run.TaskID || next.Team != "qa" {
		t.Fatalf("next = %+v", next)
	}
	text := next.Task.Text()
	for _, want := range []string{"Task " + parent.Run.TaskID.String() + " of bot qa-jefe in team qa", "running for 6m0s", "no step", "It was asked: Find out what the other bot knows"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if after := watchTasks(t, h, watcher.ExecutionID, next.Cursor); !after.Done {
		t.Errorf("after the task = %+v", after)
	}
}

// Stuck cancels the task with Jev's verdict, audits task.stuck, and tells
// the team warn.stuck: one teammate takes the work over. Wait changes
// nothing; abandon is no choice for a task, stuck none for a question.
func TestAWatcherJudgesATeamTaskStuckAndTheTeamTakesItOver(t *testing.T) {
	h, parent, watcher := watchingWork(t)
	ctx := context.Background()
	h.clock.now = t0.Add(6 * time.Minute)
	task := parent.Run.TaskID
	if waited := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: task, Choice: "wait"}); waited.Stuck || waited.Status != domain.TaskRunning {
		t.Errorf("wait = %+v", waited)
	}
	if _, err := NewDecideWatch(h.deps).Execute(ctx, "jev-node", watcher.ExecutionID, WatchDecision{Question: task, Choice: "abandon"}); err == nil {
		t.Error("a task was abandoned")
	}
	decided := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: task, Choice: "stuck", Probability: 0.9, Confidence: 0.8})
	if !decided.Stuck || decided.Status != domain.TaskCancelled || !strings.Contains(decided.Reason, "stuck, judged by qa-vigia") {
		t.Fatalf("decided = %+v", decided)
	}
	if !hasAudit(h, "task.stuck", domain.AuditOK) {
		t.Error("no task.stuck audit")
	}
	var takeover domain.Task
	for _, candidate := range h.tasks.tasks {
		if candidate.BotID() == "qa-triaje" && candidate.Trigger().Type == domain.TriggerEvent && candidate.Trigger().Name == "warn.stuck" {
			takeover = candidate
		}
	}
	if takeover.ID() == "" || !strings.Contains(takeover.Objective().String(), "qa-jefe") {
		t.Fatalf("no teammate took the work over: %+v", h.tasks.tasks)
	}
	// The teammate reads what was cut: the stuck task's trigger rides in the event.
	var input struct {
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(takeover.Input(), &input); err != nil || input.Data["trigger"] != "manual" || !strings.Contains(input.Message, "(manual)") {
		t.Errorf("takeover input = %s %v", takeover.Input(), err)
	}
	if late := decideWatch(t, h, watcher.ExecutionID, WatchDecision{Question: task, Choice: "stuck"}); late.Stuck || late.Status != domain.TaskCancelled {
		t.Errorf("late = %+v", late)
	}
}

func TestStuckIsNoChoiceForAQuestion(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Which release fixed the importer?")
	h.clock.now = t0.Add(6 * time.Minute)
	if _, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: domain.TaskID(q.TaskID), Choice: "stuck"}); err == nil {
		t.Error("a question was judged stuck")
	}
}

// A team task that has run less than five minutes is refused.
func TestAYoungTeamTaskIsNotJudged(t *testing.T) {
	h, parent, watcher := watchingWork(t)
	h.clock.now = t0.Add(2 * time.Minute)
	if _, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: parent.Run.TaskID, Choice: "stuck"}); err == nil {
		t.Error("a task of 2 minutes was judged")
	}
}
