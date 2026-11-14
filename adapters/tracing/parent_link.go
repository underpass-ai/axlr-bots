package tracing

import "go.opentelemetry.io/otel/trace"

// parentLink says where a delegated task hangs: under the parent's
// execution span when the parent task still exists, or as a link to it.
type parentLink struct {
	span   trace.SpanContext
	exists bool
}
