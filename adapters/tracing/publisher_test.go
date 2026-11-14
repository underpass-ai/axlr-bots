package tracing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

type recordingPublisher struct {
	mu     sync.Mutex
	events []domain.Event
	err    error
}

func (r *recordingPublisher) Publish(_ context.Context, event domain.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

func TestPublisherPassesEveryEventOnAndTracesTheEndedTask(t *testing.T) {
	h := newHarness(t)
	task := approvalTask(t, h, "task-1", "corr")
	inner := &recordingPublisher{}
	var logs []string
	var mu sync.Mutex
	publisher := NewPublisher(inner, h.trace, func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	ctx := context.Background()
	for _, kind := range []domain.EventType{domain.EventTaskSubmitted, domain.EventTaskStarted, domain.EventTaskProgress, domain.EventTaskWaitingApproval} {
		ok(t, publisher.Publish(ctx, domain.Event{Type: kind, TaskID: task.ID()}))
	}
	publisher.Wait()
	if n := len(h.exporter.GetSpans()); n != 0 {
		t.Fatalf("%d spans before the task ended", n)
	}
	// A cancel and its preemption end a task once.
	ok(t, publisher.Publish(ctx, domain.Event{Type: domain.EventTaskCompleted, TaskID: task.ID()}))
	ok(t, publisher.Publish(ctx, domain.Event{Type: domain.EventTaskPreempted, TaskID: task.ID()}))
	publisher.Wait()
	if len(inner.events) != 6 {
		t.Fatalf("inner saw %d events", len(inner.events))
	}
	if n := len(h.exporter.GetSpans()); n != 7 {
		t.Fatalf("%d spans, want one trace of 7", n)
	}
	if len(logs) != 0 {
		t.Fatalf("logs %v", logs)
	}
}

func TestPublisherReturnsTheInnerErrorAndTracesNothing(t *testing.T) {
	h := newHarness(t)
	task := approvalTask(t, h, "task-1", "corr")
	publisher := NewPublisher(&recordingPublisher{err: errors.New("bus down")}, h.trace, nil)
	if err := publisher.Publish(context.Background(), domain.Event{Type: domain.EventTaskCompleted, TaskID: task.ID()}); err == nil {
		t.Fatal("the inner error is the publisher's")
	}
	publisher.Wait()
	if len(h.exporter.GetSpans()) != 0 {
		t.Fatal("an event that was not published traces nothing")
	}
}

func TestPublisherLogsAFailedBuildAndTriesAgainOnTheNextEnd(t *testing.T) {
	h := newHarness(t)
	task := approvalTask(t, h, "task-1", "corr")
	h.source.failTask = errors.New("disk")
	var logs []string
	var mu sync.Mutex
	publisher := NewPublisher(&recordingPublisher{}, h.trace, func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	ctx := context.Background()
	ok(t, publisher.Publish(ctx, domain.Event{Type: domain.EventTaskCompleted, TaskID: task.ID()}))
	publisher.Wait()
	if len(logs) != 1 {
		t.Fatalf("logs %v", logs)
	}
	h.source.failTask = nil
	ok(t, publisher.Publish(ctx, domain.Event{Type: domain.EventTaskPreempted, TaskID: task.ID()}))
	publisher.Wait()
	if len(h.exporter.GetSpans()) != 7 {
		t.Fatal("the second ending traces the task")
	}
}

func TestPublisherForgetsTheOldestTasks(t *testing.T) {
	publisher := NewPublisher(&recordingPublisher{}, TaskTrace{}, nil)
	for i := 0; i <= rememberedTasks; i++ {
		if !publisher.remember(domain.TaskID(fmt.Sprintf("t-%d", i))) {
			t.Fatal("new task")
		}
	}
	if publisher.remember(domain.TaskID(fmt.Sprintf("t-%d", rememberedTasks))) {
		t.Fatal("the newest is remembered")
	}
	if !publisher.remember("t-0") {
		t.Fatal("the oldest was forgotten")
	}
}

func TestEndsTask(t *testing.T) {
	for kind, want := range map[domain.EventType]bool{
		domain.EventTaskCompleted: true, domain.EventTaskFailed: true, domain.EventTaskCancelled: true, domain.EventTaskPreempted: true, domain.EventTaskExpired: true,
		domain.EventTaskStarted: false, domain.EventTaskDecided: false, domain.EventApprovalExpired: false,
	} {
		if endsTask(kind) != want {
			t.Fatalf("%s: %v", kind, !want)
		}
	}
}

func TestToolCallsPairByCallIDAndReadGenericPayloads(t *testing.T) {
	events := []domain.Event{
		// What the event log hands back after a trip through JSON.
		{Type: domain.EventTaskProgress, ExecutionID: "x1", Payload: map[string]any{"items": []any{
			map[string]any{"kind": "tool.started", "at": t0.Format("2006-01-02T15:04:05Z07:00"), "tool": "a", "callId": "1"},
			map[string]any{"kind": "tool.ended", "at": t0.Add(2 * 1e9).Format("2006-01-02T15:04:05Z07:00"), "tool": "a", "callId": "1", "status": "ok"},
			map[string]any{"kind": "tool.ended", "at": t0.Add(9 * 1e9).Format("2006-01-02T15:04:05Z07:00"), "tool": "b", "callId": "2", "durationMs": 3000},
			map[string]any{"kind": "text", "at": t0.Format("2006-01-02T15:04:05Z07:00"), "text": "hello"},
		}}},
		{Type: domain.EventTaskProgress, ExecutionID: "x2", Payload: map[string]any{"items": []application.ProgressItem{{Kind: application.ProgressToolStarted, Tool: "other", CallID: "1", At: t0}}}},
		{Type: domain.EventTaskProgress, ExecutionID: "x1", Payload: map[string]any{"items": "not a list"}},
		{Type: domain.EventTaskSaid, ExecutionID: "x1"},
	}
	calls := toolCallsOf(events, "x1")
	if len(calls) != 2 {
		t.Fatalf("calls %+v", calls)
	}
	if calls[0].open || calls[0].end.Sub(calls[0].start).Seconds() != 2 || calls[0].status != "ok" {
		t.Fatalf("first %+v", calls[0])
	}
	if calls[1].start.Sub(t0).Seconds() != 6 || calls[1].end.Sub(t0).Seconds() != 9 {
		t.Fatalf("an end with no start is placed by its duration: %+v", calls[1])
	}
	if len(toolCallsOf(events, "x2")) != 1 || !toolCallsOf(events, "x2")[0].open {
		t.Fatal("an execution's calls are its own")
	}
	if len(progressItems(domain.Event{Payload: map[string]any{"items": make(chan int)}})) != 0 {
		t.Fatal("an undecodable payload has no items")
	}
}
