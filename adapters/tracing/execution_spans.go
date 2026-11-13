package tracing

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/underpass-ai/axlr-bots/domain"
)

// executionSpans exports the span of one execution and, under it, one
// span per tool call its run made.
func (b TaskTrace) executionSpans(ctx context.Context, traceID trace.TraceID, execution domain.Execution, events []domain.Event, taskEnd time.Time) {
	start := execution.StartedAt()
	end := execution.EndedAt()
	if end.IsZero() {
		// An execution the task ended over (cancelled before it began, or
		// a report that never came) ends with the task.
		end = taskEnd
	}
	end = atLeast(end, start)
	spanID := spanIDOf("execution", execution.ID().String())
	execCtx, span := b.tracer.Start(withIDs(ctx, traceID, spanID), "axlr.execution",
		trace.WithTimestamp(start), trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(executionAttributes(execution)...))
	for index, call := range toolCallsOf(events, execution.ID()) {
		b.toolSpan(execCtx, traceID, execution.ID(), index, call, end)
	}
	if execution.Outcome() == domain.ExecutionFailed {
		span.SetStatus(codes.Error, execution.FailureReason())
	}
	span.End(trace.WithTimestamp(end))
}

func (b TaskTrace) toolSpan(ctx context.Context, traceID trace.TraceID, execution domain.ExecutionID, index int, call toolCall, executionEnd time.Time) {
	name := "axlr.tool"
	if call.tool != "" {
		name += " " + call.tool
	}
	end := call.end
	if call.open {
		end = executionEnd
	}
	end = atLeast(end, call.start)
	attributes := []attribute.KeyValue{
		attribute.String("axlr.execution", execution.String()),
		attribute.String("axlr.tool", call.tool),
	}
	for key, value := range map[string]string{"axlr.mcp_tool": call.mcpTool, "axlr.call_id": call.callID, "axlr.tool_summary": call.summary, "outcome": call.status} {
		if value != "" {
			attributes = append(attributes, attribute.String(key, value))
		}
	}
	if call.open {
		attributes = append(attributes, attribute.Bool("axlr.tool_unfinished", true))
	}
	_, span := b.tracer.Start(withIDs(ctx, traceID, spanIDOf("tool", execution.String()+"/"+strconv.Itoa(index))), name,
		trace.WithTimestamp(call.start), trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attributes...))
	if call.status == "failed" || call.status == "denied" {
		span.SetStatus(codes.Error, call.err)
	}
	span.End(trace.WithTimestamp(end))
}

// approvalSpan exports the span of the wait for a person's decision
// when the task has one that ended: from the moment it stopped on the
// call to the moment a person (or the deadline) closed it.
func (b TaskTrace) approvalSpan(ctx context.Context, traceID trace.TraceID, task domain.Task) {
	wait, waited := task.Approval()
	if !waited || wait.Open() || wait.ClosedAt == nil {
		return
	}
	attributes := []attribute.KeyValue{
		attribute.String("axlr.task", task.ID().String()),
		attribute.String("axlr.tool", wait.Tool),
		attribute.String("axlr.call_id", wait.CallID),
		attribute.String("outcome", wait.Outcome),
		attribute.String("axlr.approval_deadline", wait.Deadline.UTC().Format(time.RFC3339)),
	}
	_, span := b.tracer.Start(withIDs(ctx, traceID, spanIDOf("approval", task.ID().String())), "approval.wait",
		trace.WithTimestamp(wait.Since), trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attributes...))
	if wait.Outcome == domain.ApprovalDenied || wait.Outcome == domain.ApprovalExpired {
		span.SetStatus(codes.Error, "approval "+wait.Outcome)
	}
	span.End(trace.WithTimestamp(atLeast(*wait.ClosedAt, wait.Since)))
}

func executionAttributes(execution domain.Execution) []attribute.KeyValue {
	usage := execution.Usage()
	attributes := []attribute.KeyValue{
		attribute.String("axlr.bot", execution.BotID().String()),
		attribute.String("axlr.task", execution.TaskID().String()),
		attribute.String("axlr.execution", execution.ID().String()),
		attribute.String("outcome", execution.Outcome().String()),
		attribute.String("axlr.execution_kind", string(execution.Kind())),
		attribute.Int("axlr.requests", usage.Requests),
		attribute.Int("axlr.tokens.prompt", usage.PromptTokens),
		attribute.Int("axlr.tokens.cached", usage.CachedTokens),
		attribute.Int("axlr.tokens.completion", usage.CompletionTokens),
		attribute.Int("axlr.tokens.reasoning", usage.ReasoningTokens),
		attribute.Int("axlr.tool_calls", usage.ToolCalls),
		attribute.Bool("axlr.cost_unknown", usage.CostUnknown),
	}
	if !usage.CostUnknown {
		attributes = append(attributes, attribute.Float64("axlr.cost_usd", usage.CostUSD))
	}
	if node := execution.NodeID().String(); node != "" {
		attributes = append(attributes, attribute.String("axlr.node", node))
	}
	return attributes
}
