package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/underpass-ai/axlr-bots/domain"
)

// maxLineageDepth bounds the walk up a chain of delegated tasks; the
// platform's own limit is lower (domain.MaxDelegationDepthLimit).
const maxLineageDepth = 16

// TaskTrace builds the trace of one ended task from what the control
// plane stored, with the timestamps it stored, and hands it to the
// tracer. It holds no span between two calls.
type TaskTrace struct {
	source Source
	tracer trace.Tracer
}

// NewTaskTrace builds spans with the provider's tracer, reading from source.
func NewTaskTrace(source Source, provider trace.TracerProvider) TaskTrace {
	return TaskTrace{source: source, tracer: provider.Tracer("github.com/underpass-ai/axlr-bots/adapters/tracing")}
}

// Build exports the task's trace: the task span, one span per execution
// with one per tool call, and one for the approval wait. A task that is
// not over has none; the answer says whether a trace was built.
func (b TaskTrace) Build(ctx context.Context, id domain.TaskID) (bool, error) {
	task, found, err := b.source.Task(ctx, id)
	if err != nil || !found || !task.Status().IsTerminal() {
		return false, err
	}
	events, err := b.source.TaskEvents(ctx, id)
	if err != nil {
		return false, err
	}
	traceID, parent, err := b.placeOf(ctx, task)
	if err != nil {
		return false, err
	}
	rootID := spanIDOf("task", task.ID().String())
	rootCtx := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	var links []trace.Link
	if parent.exists {
		rootCtx = trace.ContextWithSpanContext(rootCtx, parent.span)
	} else if parent.span.IsValid() {
		links = append(links, trace.Link{SpanContext: parent.span, Attributes: []attribute.KeyValue{attribute.String("axlr.link", "delegated_from")}})
	}
	created, ended := task.CreatedAt(), task.UpdatedAt()
	if ended.Before(created) {
		ended = created
	}
	rootCtx, root := b.tracer.Start(withIDs(rootCtx, traceID, rootID), "axlr.task",
		trace.WithTimestamp(created), trace.WithSpanKind(trace.SpanKindInternal), trace.WithLinks(links...),
		trace.WithAttributes(taskAttributes(task)...))
	for _, executionID := range task.ExecutionIDs() {
		execution, err := b.source.Execution(ctx, executionID)
		if err != nil {
			root.RecordError(err)
			continue
		}
		b.executionSpans(rootCtx, traceID, execution, events, ended)
	}
	b.approvalSpan(rootCtx, traceID, task)
	if status := task.Status(); status == domain.TaskFailed || status == domain.TaskExpired {
		root.SetStatus(codes.Error, task.FailureReason())
	}
	root.End(trace.WithTimestamp(ended))
	return true, nil
}

// placeOf says the task's trace id and where it hangs. A task nobody
// delegated takes its trace id from its correlation id, or from its own
// id without one. A delegated task whose parent still exists is in the
// parent's trace and under the parent's execution span; one whose parent
// is gone has the trace id its own correlation id (or its chain's top
// task) gives and a link to the span the parent had.
func (b TaskTrace) placeOf(ctx context.Context, task domain.Task) (trace.TraceID, parentLink, error) {
	traceID, err := b.traceOf(ctx, task, 0)
	if err != nil {
		return trace.TraceID{}, parentLink{}, err
	}
	lineage, delegated := task.Lineage()
	if !delegated {
		return traceID, parentLink{}, nil
	}
	link := parentLink{span: spanContext(traceID, spanIDOf("execution", lineage.ParentExecutionID))}
	if parentID, err := domain.NewTaskID(lineage.ParentTaskID); err == nil {
		_, link.exists, err = b.source.Task(ctx, parentID)
		if err != nil {
			return trace.TraceID{}, parentLink{}, err
		}
	}
	return traceID, link, nil
}

// traceOf is the trace id of a task: its parent's when the parent
// exists, else the one its own correlation id (or the top of its chain)
// gives.
func (b TaskTrace) traceOf(ctx context.Context, task domain.Task, depth int) (trace.TraceID, error) {
	lineage, delegated := task.Lineage()
	if !delegated || depth >= maxLineageDepth {
		return ownTraceID(task, task.ID().String()), nil
	}
	if parentID, err := domain.NewTaskID(lineage.ParentTaskID); err == nil {
		parent, found, err := b.source.Task(ctx, parentID)
		if err != nil {
			return trace.TraceID{}, err
		}
		if found {
			return b.traceOf(ctx, parent, depth+1)
		}
	}
	return ownTraceID(task, b.topOfChain(ctx, lineage)), nil
}

// topOfChain is the id of the highest task of the chain that is still
// stored, or the highest the lineage names when none above is.
func (b TaskTrace) topOfChain(ctx context.Context, lineage domain.TaskLineage) string {
	top := lineage.ParentTaskID
	for range maxLineageDepth {
		id, err := domain.NewTaskID(top)
		if err != nil {
			return top
		}
		task, found, err := b.source.Task(ctx, id)
		if err != nil || !found {
			return top
		}
		above, delegated := task.Lineage()
		if !delegated {
			return top
		}
		top = above.ParentTaskID
	}
	return top
}

// ownTraceID is the trace id a task's own correlation id gives, or the
// id of the top of its chain when it has none.
func ownTraceID(task domain.Task, top string) trace.TraceID {
	if correlation := task.CorrelationID().String(); correlation != "" {
		return traceIDFromCorrelation(correlation)
	}
	return traceIDFromRoot(top)
}

func spanContext(traceID trace.TraceID, spanID trace.SpanID) trace.SpanContext {
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, Remote: true})
}

func taskAttributes(task domain.Task) []attribute.KeyValue {
	attributes := []attribute.KeyValue{
		attribute.String("axlr.bot", task.BotID().String()),
		attribute.String("axlr.task", task.ID().String()),
		attribute.String("outcome", task.Status().String()),
	}
	if correlation := task.CorrelationID().String(); correlation != "" {
		attributes = append(attributes, attribute.String("axlr.correlation_id", correlation))
	}
	if lineage, delegated := task.Lineage(); delegated {
		attributes = append(attributes,
			attribute.String("axlr.parent_task", lineage.ParentTaskID),
			attribute.String("axlr.parent_bot", lineage.ParentBotID),
			attribute.Int("axlr.delegation_depth", lineage.Depth))
	}
	return attributes
}

// atLeast keeps a span from ending before it began.
func atLeast(t, floor time.Time) time.Time {
	if t.Before(floor) {
		return floor
	}
	return t
}
