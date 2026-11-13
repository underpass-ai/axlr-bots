package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

type taskPort struct {
	application.TaskRepository
	task domain.Task
	err  error
}

func (p taskPort) Get(_ context.Context, id domain.TaskID) (domain.Task, error) {
	if p.err != nil {
		return domain.Task{}, p.err
	}
	if id != p.task.ID() {
		return domain.Task{}, domain.NotFoundError{Kind: "task", ID: id.String()}
	}
	return p.task, nil
}

type executionPort struct {
	application.ExecutionRepository
	execution domain.Execution
}

func (p executionPort) Get(context.Context, domain.ExecutionID) (domain.Execution, error) {
	return p.execution, nil
}

type eventPort struct {
	application.EventReader
	asked int
}

func (p *eventPort) ListTaskEvents(_ context.Context, _ domain.TaskID, limit int) ([]domain.Event, error) {
	p.asked = limit
	return []domain.Event{{Type: domain.EventTaskStarted}}, nil
}

func TestSourceAnswersFromThePorts(t *testing.T) {
	h := newHarness(t)
	task := h.newTask("task-1", "c")
	execution := h.execution("x1", task, t0)
	events := &eventPort{}
	source := NewSource(taskPort{task: task}, executionPort{execution: execution}, events)
	ctx := context.Background()
	if got, found, err := source.Task(ctx, "task-1"); err != nil || !found || got.ID() != "task-1" {
		t.Fatalf("Task = %v %v %v", got.ID(), found, err)
	}
	if _, found, err := source.Task(ctx, "other"); err != nil || found {
		t.Fatalf("a missing task is not an error: %v %v", found, err)
	}
	if got, err := source.Execution(ctx, "x1"); err != nil || got.ID() != "x1" {
		t.Fatal(got.ID(), err)
	}
	if got, err := source.TaskEvents(ctx, "task-1"); err != nil || len(got) != 1 || events.asked != application.MaxEventLimit {
		t.Fatalf("TaskEvents = %v %v asked %d", got, err, events.asked)
	}
	boom := errors.New("disk")
	if _, _, err := NewSource(taskPort{err: boom}, nil, nil).Task(ctx, "task-1"); !errors.Is(err, boom) {
		t.Fatalf("a read error is returned: %v", err)
	}
}
