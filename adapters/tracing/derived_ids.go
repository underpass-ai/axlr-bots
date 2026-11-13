package tracing

import (
	"crypto/sha256"

	"go.opentelemetry.io/otel/trace"
)

// traceIDFromCorrelation derives the trace id of the work a correlation
// id names. The correlation id is free text (up to 128 characters), not
// a trace id: the same text always gives the same trace id, and the text
// itself goes on the spans as axlr.correlation_id.
func traceIDFromCorrelation(correlation string) trace.TraceID {
	return traceIDOf("correlation:" + correlation)
}

// traceIDFromRoot derives the trace id of a task chain nobody gave a
// correlation id, from the id of the chain's first task.
func traceIDFromRoot(task string) trace.TraceID {
	return traceIDOf("root-task:" + task)
}

func traceIDOf(seed string) trace.TraceID {
	sum := sha256.Sum256([]byte(seed))
	var id trace.TraceID
	copy(id[:], sum[:])
	return id
}

// spanIDOf derives a span id from what the span is (task, execution,
// tool, approval) and its identity, so a rebuilt trace has the same ids.
func spanIDOf(kind, identity string) trace.SpanID {
	sum := sha256.Sum256([]byte(kind + ":" + identity))
	var id trace.SpanID
	copy(id[:], sum[:])
	return id
}
