package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Every task writer waits for the state lock: while another holds it,
// none of them reads its task (QA L8).
func TestSerialRunnerTaskWritersWaitForTheStateLock(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	hooked := triggeredBot(t, h)
	bot := hooked
	runner := NewSerialRunner(h.deps)
	pending := submit(t, h, bot, "k-0").Task
	writers := map[string]func() error{
		"submit": func() error {
			_, err := runner.SubmitTask(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "Look", IdempotencyKey: "k-1"})
			return err
		},
		"fire": func() error {
			_, err := runner.FireTrigger(ctx, FireTriggerCommand{BotID: hooked.ID().String(), TriggerName: "deploy", DeliveryID: "d-1", Body: []byte(`{"type":"deploy","service":"web"}`)})
			return err
		},
		"message": func() error {
			_, err := runner.SendTaskMessage(ctx, pending.ID(), "hello")
			var refused domain.StateError
			if errors.As(err, &refused) {
				return nil // Pending: refused, but only once the lock was free.
			}
			return err
		},
		"cancel": func() error { _, err := runner.CancelTask(ctx, pending.ID()); return err },
	}
	for _, name := range []string{"submit", "fire", "message", "cancel"} {
		runner.mu.Lock()
		done := make(chan error, 1)
		go func() { done <- writers[name]() }()
		select {
		case err := <-done:
			t.Fatalf("%s ran while the lock was held: %v", name, err)
		case <-time.After(20 * time.Millisecond):
		}
		runner.mu.Unlock()
		if err := <-done; err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// settlingTasks holds the save of a Completed task until released: the
// moment a node's report has saved the execution Completed and not yet
// the task.
type settlingTasks struct {
	TaskRepository
	saving  chan struct{}
	release chan struct{}
}

func (s settlingTasks) Save(ctx context.Context, task domain.Task) error {
	if task.Status() == domain.TaskCompleted {
		close(s.saving)
		<-s.release
	}
	return s.TaskRepository.Save(ctx, task)
}

// A cancel that comes while a node's report settles the task waits for
// it, and then finds the task Completed: it no longer saved the task
// Cancelled over the report, with both events (QA L8, 5 November 2026).
func TestACancelDuringAReportNeverOverwritesTheCompletedTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	bot := enabledBot(t, h)
	assignment := runOnNode(t, h, bot, "web-1", "k-run")
	tasks := settlingTasks{TaskRepository: h.tasks, saving: make(chan struct{}), release: make(chan struct{})}
	deps := h.deps
	deps.Tasks = tasks
	runner := NewSerialRunner(deps)
	reported := make(chan error, 1)
	go func() {
		_, err := runner.Report(ctx, "web-1", assignment.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}})
		reported <- err
	}()
	<-tasks.saving
	execution, _ := h.execs.Get(ctx, assignment.ExecutionID)
	if execution.Outcome() != domain.ExecutionCompleted {
		t.Fatalf("the report has not saved the execution yet: %s", execution.Outcome())
	}
	cancelled := make(chan error, 1)
	go func() {
		_, err := runner.CancelTask(ctx, assignment.Run.TaskID)
		cancelled <- err
	}()
	select {
	case err := <-cancelled:
		t.Fatalf("the cancel ran while the report settled: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(tasks.release)
	if err := <-reported; err != nil {
		t.Fatal(err)
	}
	var refused domain.StateError
	if err := <-cancelled; !errors.As(err, &refused) {
		t.Errorf("cancel = %v, want refused: the task had completed", err)
	}
	task, _ := h.tasks.Get(ctx, assignment.Run.TaskID)
	if task.Status() != domain.TaskCompleted {
		t.Errorf("task = %s", task.Status())
	}
	for _, event := range h.log.events {
		if event.Type == domain.EventTaskCancelled {
			t.Errorf("a cancelled event for a completed task: %+v", event)
		}
	}
}
