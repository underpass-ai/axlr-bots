package tracing

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"go.opentelemetry.io/otel/sdk/resource"
)

// ServiceName is the service.name resource attribute of every span.
const ServiceName = "axlr-bots-control-plane"

// SpanExporter is where finished spans go.
type SpanExporter = sdktrace.SpanExporter

// NewOTLPExporter sends spans to an OTLP/HTTP endpoint, such as
// http://tempo:4318 (the path /v1/traces is added unless the URL has
// one). HTTP rather than gRPC: one request per batch over the standard
// library's client, which passes through any proxy or ingress with no
// long-lived stream to keep healthy, and which Tempo and every collector
// accept on 4318. It does not keep gRPC out of the binary: the HTTP
// exporter's configuration package imports it (docs/architecture/tracing.md
// has the measured cost).
func NewOTLPExporter(ctx context.Context, endpoint string) (SpanExporter, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("tracing: the OTLP endpoint must be http://host:port or https://host:port, got %q", endpoint)
	}
	options := []otlptracehttp.Option{otlptracehttp.WithEndpoint(parsed.Host)}
	if parsed.Path != "" && parsed.Path != "/" {
		options = append(options, otlptracehttp.WithURLPath(parsed.Path))
	}
	if parsed.Scheme == "http" {
		options = append(options, otlptracehttp.WithInsecure())
	}
	return otlptracehttp.New(ctx, options...)
}

// NewProvider is the tracer provider of the control plane: the service
// name as the resource, every span sampled (a trace is built only for a
// task that ended, never in bulk), and the adapter's own ids. Batched
// sends in the background, as production does; unbatched exports each
// span as it ends, for a test that reads them at once.
func NewProvider(exporter SpanExporter, batched bool) *sdktrace.TracerProvider {
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", ServiceName))),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithIDGenerator(idGenerator{}),
	}
	if batched {
		options = append(options, sdktrace.WithBatcher(exporter))
	} else {
		options = append(options, sdktrace.WithSyncer(exporter))
	}
	return sdktrace.NewTracerProvider(options...)
}
