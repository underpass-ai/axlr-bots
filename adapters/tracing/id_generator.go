package tracing

import (
	"context"
	"crypto/rand"

	"go.opentelemetry.io/otel/trace"
)

// idGenerator hands the SDK the ids the adapter asked for through the
// context, so a span built after the fact has the id its children (and
// a task delegated from it, in another trace build) already name. A
// span started without them gets random ones.
type idGenerator struct{}

type wantedIDs struct {
	trace trace.TraceID
	span  trace.SpanID
}

type wantedIDsKey struct{}

// withIDs asks for the trace and span id of the next span started on ctx.
func withIDs(ctx context.Context, traceID trace.TraceID, spanID trace.SpanID) context.Context {
	return context.WithValue(ctx, wantedIDsKey{}, wantedIDs{trace: traceID, span: spanID})
}

func (idGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if wanted, ok := ctx.Value(wantedIDsKey{}).(wantedIDs); ok {
		return wanted.trace, wanted.span
	}
	var traceID trace.TraceID
	_, _ = rand.Read(traceID[:])
	return traceID, randomSpanID()
}

func (idGenerator) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	if wanted, ok := ctx.Value(wantedIDsKey{}).(wantedIDs); ok {
		return wanted.span
	}
	return randomSpanID()
}

func randomSpanID() trace.SpanID {
	var id trace.SpanID
	_, _ = rand.Read(id[:])
	return id
}
