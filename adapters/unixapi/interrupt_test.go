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
	client, _, _ := serveWith(t, "completed", func(deps *application.Dependencies) { deps.Runtime = runtime })
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, definition("bot-i", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-i"); err != nil {
		t.Fatal(err)
	}
	run := func(key string) <-chan RunResponse {
		t.Helper()
		submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-i", Objective: "take a while", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		ran := make(chan RunResponse, 1)
		go func() {
			result, _ := client.RunTask(ctx, submitted.Task.ID)
			ran <- result
		}()
		select {
		case <-runtime.started:
		case <-time.After(5 * time.Second):
			t.Fatal("the run never started")
		}
		return ran
	}
	ended := func(what string, ran <-chan RunResponse) {
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
	if stopped, err := client.StopBot(ctx, "bot-i"); err != nil || len(stopped.Cancelled) != 1 {
		t.Fatalf("stop: %v %+v", err, stopped)
	}
	ended("stop", ran)
	if _, err := client.EnableBot(ctx, "bot-i"); err != nil {
		t.Fatal(err)
	}
	ran = run("slow-2")
	if _, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-i", Objective: "now", IdempotencyKey: "urgent-1", Priority: "urgent"}); err != nil {
		t.Fatal(err)
	}
	ended("urgent", ran)
}
