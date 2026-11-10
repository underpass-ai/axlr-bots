package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func readQuestion(t *testing.T, h *harness, node domain.NodeID, execution domain.ExecutionID, question domain.TaskID) QuestionView {
	t.Helper()
	view, err := NewGetQuestion(h.deps).Execute(context.Background(), node, execution, question)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// The asker reads where its question stands, from the records alone:
// queued and why, running and its last step, waiting for a person, ended.
func TestTheAskerReadsWhereItsQuestionStands(t *testing.T) {
	h, parent := channel(t)
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(42 * time.Minute)
	view := readQuestion(t, h, "qa-node", parent.ExecutionID, q.TaskID)
	if view.Status != domain.TaskPending || view.Team != "dev" || view.AskedBy != "qa-jefe" || view.Responder != "dev-a" || view.Waiting != "its turn on the node has not come" {
		t.Fatalf("pending = %+v", view)
	}
	text := view.Text()
	for _, want := range []string{"from qa-jefe to team dev, asked 42m0s ago; status Pending", "Responder dev-a has 0 other open tasks", "Deadline in 1h18m0s"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	// The responder busy with another task.
	if _, err := NewSubmitTask(h.deps).Execute(context.Background(), SubmitTaskCommand{BotID: "dev-a", Objective: "Fix the build", IdempotencyKey: "build-1"}); err != nil {
		t.Fatal(err)
	}
	if busy := readQuestion(t, h, "qa-node", parent.ExecutionID, q.TaskID); busy.OpenTasks != 1 || busy.Waiting != "its responder is busy with 1 other tasks" {
		t.Errorf("busy = %+v", busy)
	}
	// Running, with its last step.
	got := nextFor(t, h, "dev-node")
	if got.Run.TaskID != q.TaskID {
		// The build went first: run it out of the way, then the question.
		if _, err := NewReportExecution(h.deps).Execute(context.Background(), "dev-node", got.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "built"}}); err != nil {
			t.Fatal(err)
		}
		got = nextFor(t, h, "dev-node")
	}
	progress, _ := domain.NewEvent("evt-p1", domain.EventTaskProgress, "default", "dev-a", q.TaskID, "", h.clock.now, map[string]any{"items": []any{map[string]any{"kind": "tool_call", "tool": "kmp_wake", "status": "ok"}}})
	h.log.events = append(h.log.events, progress)
	h.clock.now = h.clock.now.Add(3 * time.Minute)
	running := readQuestion(t, h, "qa-node", parent.ExecutionID, q.TaskID)
	if running.Status != domain.TaskRunning || running.RunningSince.IsZero() || running.LastStep != "tool_call kmp_wake ok" || !strings.Contains(running.Text(), "Last step 3m0s ago: tool_call kmp_wake ok.") {
		t.Errorf("running = %+v\n%s", running, running.Text())
	}
	// Ended without an answer: the reason, no deadline.
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "dev-node", got.ExecutionID, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "the model gave up"}); err != nil {
		t.Fatal(err)
	}
	ended := readQuestion(t, h, "qa-node", parent.ExecutionID, q.TaskID)
	if ended.Status != domain.TaskFailed || !strings.Contains(ended.Reason, "the model gave up") || strings.Contains(ended.Text(), "Deadline") {
		t.Errorf("ended = %+v\n%s", ended, ended.Text())
	}
}

// A question waits when no node that shares the team's memory is Online,
// or when its responder takes no task.
func TestAQueuedQuestionSaysWhatItWaitsFor(t *testing.T) {
	h := newHarness()
	channelNode(t, h, "qa-node")
	channelNode(t, h, "dev-node", "dev")
	asker := channelBot(t, h, "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}, nil, "qa-node")
	channelBot(t, h, "dev-a", []string{"dev"}, nil, []string{"qa"})
	parent := runOnNode(t, h, asker, "qa-node", "lead-1")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who?")
	if _, err := NewRevokeNode(h.deps).Execute(context.Background(), "dev-node"); err != nil {
		t.Fatal(err)
	}
	if view := readQuestion(t, h, "qa-node", parent.ExecutionID, q.TaskID); view.Waiting != "no Online node that shares team dev's memory" {
		t.Errorf("no node = %+v", view)
	}
	if _, err := NewPauseBot(h.deps).Execute(context.Background(), "dev-a"); err != nil {
		t.Fatal(err)
	}
	if view := readQuestion(t, h, "qa-node", parent.ExecutionID, q.TaskID); view.Waiting != "its responder is Paused" {
		t.Errorf("paused = %+v", view)
	}
}

// A node reads only the questions of the task whose execution it holds.
func TestANodeReadsOnlyItsTasksQuestions(t *testing.T) {
	h, parent := channel(t)
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who?")
	other := channelBot(t, h, "qa-otro", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}, nil, "qa-node")
	e := runOnNode(t, h, other, "qa-node", "otro-1")
	if _, err := NewGetQuestion(h.deps).Execute(context.Background(), "qa-node", e.ExecutionID, q.TaskID); !domain.IsNotFound(err) {
		t.Errorf("another task's question: %v", err)
	}
	if _, err := NewGetQuestion(h.deps).Execute(context.Background(), "dev-node", parent.ExecutionID, q.TaskID); err == nil {
		t.Error("another node read it")
	}
	if _, err := readQuestionView(context.Background(), h.deps, mustTask(t, h, parent.Run.TaskID)); !domain.IsNotFound(err) {
		t.Errorf("a task that is no question: %v", err)
	}
}

func mustTask(t *testing.T, h *harness, id domain.TaskID) domain.Task {
	t.Helper()
	task, err := h.tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
