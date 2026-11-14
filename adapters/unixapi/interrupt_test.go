package unixapi

import (
	"context"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// blockingRuntime runs until its context ends, as a long axlr-tui run
// that honours its interruption does.
type blockingRuntime struct{ started chan struct{} }

func (b blockingRuntime) Execute(ctx context.Context, _ application.ExecutionRequest) (application.ExecutionReport, error) {
	b.started <- struct{}{}
	select {
	case <-ctx.Done():
		return application.ExecutionReport{}, ctx.Err()
	case <-time.After(30 * time.Second):
		return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ran to its end"}}, nil
	}
}

// A task an in-process run carries ends that run however it was ended:
// a bot's stop and an urgent task cancel it without the cancel route,
// and the run must not go on to its own end, holding the in-process
// worker (QA 5 Nov 2026: a stopped bot's run went on for its timeout).
func TestStopAndPreemptionInterruptAnInProcessRun(t *testing.T) {
	runtime := blockingRuntime{started: make(chan struct{}, 4)}
	_, local, _ := serveWith(t, "completed", func(deps *application.Dependencies) { deps.Runtime = runtime })
	ctx := context.Background()
	if _, err := local.RegisterBot(ctx, definition("bot-i", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "bot-i"); err != nil {
		t.Fatal(err)
	}
	run := func(key string) <-chan application.RunResponse {
		t.Helper()
		submitted, err := local.SubmitTask(ctx, application.SubmitRequest{BotID: "bot-i", Objective: "take a while", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		ran := make(chan application.RunResponse, 1)
		go func() {
			result, _ := local.RunTask(ctx, submitted.Task.ID)
			ran <- result
		}()
		select {
		case <-runtime.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the run never started")
		}
		return ran
	}
	ended := func(what string, ran <-chan application.RunResponse) {
		t.Helper()
		select {
		case result := <-ran:
			if result.Task.Status != "Cancelled" {
				t.Errorf("%s: run ended %s / %s", what, result.Task.Status, result.Execution.Outcome)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the in-process run was not interrupted", what)
		}
	}
	ran := run("stop-1")
	if stopped, err := local.StopBot(ctx, "bot-i"); err != nil || len(stopped.Cancelled) != 1 {
		t.Fatalf("stop: %v %+v", err, stopped)
	}
	ended("stop", ran)
	if _, err := local.EnableBot(ctx, "bot-i"); err != nil {
		t.Fatal(err)
	}
	ran = run("slow-2")
	if _, err := local.SubmitTask(ctx, application.SubmitRequest{BotID: "bot-i", Objective: "now", IdempotencyKey: "urgent-1", Priority: "urgent"}); err != nil {
		t.Fatal(err)
	}
	ended("urgent", ran)
}

// Deleting a bot interrupts its in-process run as a stop does, over the
// socket: the answer names the task, the bot is gone, its task stays as
// history and the same id may be registered again.
func TestDeleteBotInterruptsAnInProcessRunAndKeepsTheHistory(t *testing.T) {
	runtime := blockingRuntime{started: make(chan struct{}, 2)}
	_, local, _ := serveWith(t, "completed", func(deps *application.Dependencies) { deps.Runtime = runtime })
	ctx := context.Background()
	if _, err := local.RegisterBot(ctx, definition("bot-d", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "bot-d"); err != nil {
		t.Fatal(err)
	}
	submitted, err := local.SubmitTask(ctx, application.SubmitRequest{BotID: "bot-d", Objective: "take a while", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan application.RunResponse, 1)
	go func() {
		result, _ := local.RunTask(ctx, submitted.Task.ID)
		ran <- result
	}()
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never started")
	}
	deleted, err := local.DeleteBot(ctx, "bot-d")
	if err != nil || len(deleted.Cancelled) != 1 || deleted.Cancelled[0] != submitted.Task.ID || deleted.Bot.ID != "bot-d" || deleted.NamedBy == nil {
		t.Fatalf("delete: %v %+v", err, deleted)
	}
	select {
	case result := <-ran:
		if result.Task.Status != "Cancelled" {
			t.Errorf("run ended %s", result.Task.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the in-process run was not interrupted")
	}
	if _, err := local.GetBot(ctx, "bot-d"); !domain.IsNotFound(err) {
		t.Errorf("get after delete: %v", err)
	}
	if task, err := local.GetTask(ctx, submitted.Task.ID); err != nil || task.FailureReason != application.DeletedReason {
		t.Errorf("task after delete: %v %+v", err, task)
	}
	if _, err := local.DeleteBot(ctx, "bot-d"); !domain.IsNotFound(err) {
		t.Errorf("second delete: %v", err)
	}
	if _, err := local.DeleteBot(ctx, "Not An Id"); err == nil {
		t.Error("a bad id was taken")
	}
	if _, err := local.RegisterBot(ctx, definition("bot-d", "observe")); err != nil {
		t.Errorf("register again: %v", err)
	}
}
