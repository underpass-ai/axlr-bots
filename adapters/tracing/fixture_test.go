package tracing

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// memorySource is a Source over maps.
type memorySource struct {
	tasks      map[domain.TaskID]domain.Task
	executions map[domain.ExecutionID]domain.Execution
	events     map[domain.TaskID][]domain.Event
	failTask   error
}

func newMemorySource() *memorySource {
	return &memorySource{tasks: map[domain.TaskID]domain.Task{}, executions: map[domain.ExecutionID]domain.Execution{}, events: map[domain.TaskID][]domain.Event{}}
}

func (m *memorySource) Task(_ context.Context, id domain.TaskID) (domain.Task, bool, error) {
	if m.failTask != nil {
		return domain.Task{}, false, m.failTask
	}
	task, ok := m.tasks[id]
	return task, ok, nil
}

func (m *memorySource) Execution(_ context.Context, id domain.ExecutionID) (domain.Execution, error) {
	execution, ok := m.executions[id]
	if !ok {
		return domain.Execution{}, domain.NotFoundError{Kind: "execution", ID: id.String()}
	}
	return execution, nil
}

func (m *memorySource) TaskEvents(_ context.Context, id domain.TaskID) ([]domain.Event, error) {
	return m.events[id], nil
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// harness builds traces into an in-memory exporter.
type harness struct {
	t        *testing.T
	source   *memorySource
	exporter *tracetest.InMemoryExporter
	provider *sdktrace.TracerProvider
	trace    TaskTrace
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := NewProvider(exporter, false)
	source := newMemorySource()
	return &harness{t: t, source: source, exporter: exporter, provider: provider, trace: NewTaskTrace(source, provider)}
}

func (h *harness) newTask(id, correlation string) domain.Task {
	h.t.Helper()
	objective, err := domain.NewObjective("do it")
	ok(h.t, err)
	task, err := domain.NewTask(domain.TaskID(id), "bot-a", objective, "", domain.CorrelationID(correlation), t0)
	ok(h.t, err)
	return task
}

func (h *harness) execution(id string, task domain.Task, start time.Time) domain.Execution {
	h.t.Helper()
	execution, err := domain.NewExecution(domain.ExecutionID(id), task.ID(), task.BotID(), start)
	ok(h.t, err)
	return execution
}

func progress(task domain.TaskID, execution domain.ExecutionID, items ...application.ProgressItem) domain.Event {
	return domain.Event{ID: domain.EventID(fmt.Sprintf("ev-%d", len(items))), Type: domain.EventTaskProgress, TaskID: task, ExecutionID: execution, Payload: map[string]any{"items": items}}
}

func (h *harness) spans() map[string]tracetest.SpanStub {
	h.t.Helper()
	out := map[string]tracetest.SpanStub{}
	for _, span := range h.exporter.GetSpans() {
		out[span.Name] = span
	}
	return out
}

type spanStub = tracetest.SpanStub
