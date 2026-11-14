package daemon

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/underpass-ai/axlr-bots/application"
)

// A task the daemon runs in process leaves one trace, with the task,
// its execution and the service name, in the exporter the options give.
func TestDaemonExportsOneTracePerEndedTask(t *testing.T) {
	f := newFixture(t, "completed")
	exporter := &keptSpans{InMemoryExporter: tracetest.NewInMemoryExporter()}
	client, stop := f.startWith(t, func(o *Options) { o.TraceExporter = exporter })
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, application.BotDefinition{Tenant: "ops", Name: "bot", Owner: "platform", Mission: "diagnose", AllowedTools: []string{"local_read"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, unixapiSubmit("k-trace"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, client, submitted.Task.ID, "Completed")
	// The task is saved before its event is published, and the daemon
	// logs the run after both: stopping then loses no trace.
	waitForLog(t, f, "task "+submitted.Task.ID+" Completed")
	stop() // waits for the trace under way and flushes the exporter
	spans := exporter.InMemoryExporter.GetSpans()
	names := map[string]int{}
	for _, span := range spans {
		names[span.Name]++
		if got, _ := serviceName(span); got != "axlr-bots-control-plane" {
			t.Errorf("span %s service.name = %q", span.Name, got)
		}
	}
	if names["axlr.task"] != 1 || names["axlr.execution"] != 1 {
		t.Fatalf("spans %v\nlogs:\n%s", names, f.logs.String())
	}
}

func TestDaemonWithoutAnEndpointCreatesNoExporter(t *testing.T) {
	f := newFixture(t, "completed")
	got, stop, err := withTracing(context.Background(), f.deps, Options{}, levelLogger{})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if got.Events != f.deps.Events {
		t.Fatal("the event publisher is not decorated without tracing")
	}
}

func TestDaemonRefusesABadOTLPEndpoint(t *testing.T) {
	f := newFixture(t, "completed")
	if _, _, err := withTracing(context.Background(), f.deps, Options{OTLPEndpoint: "tempo:4318"}, levelLogger{}); err == nil || !strings.Contains(err.Error(), "OTLP endpoint") {
		t.Fatalf("err = %v", err)
	}
}

func unixapiSubmit(key string) application.SubmitRequest {
	return application.SubmitRequest{BotID: "bot", Objective: "List the files in your workspace", IdempotencyKey: key}
}

func serviceName(span tracetest.SpanStub) (string, bool) {
	for _, kv := range span.Resource.Attributes() {
		if kv.Key == "service.name" {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// keptSpans keeps its spans through Shutdown, which the in-memory
// exporter answers by forgetting them.
type keptSpans struct{ *tracetest.InMemoryExporter }

func (keptSpans) Shutdown(context.Context) error { return nil }
