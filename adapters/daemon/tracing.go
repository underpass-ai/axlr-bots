package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/tracing"
	"github.com/underpass-ai/axlr-bots/application"
)

// flushTimeout bounds the flush of the spans still queued at shutdown.
const flushTimeout = 10 * time.Second

// withTracing decorates the event publisher so every task that ends
// leaves a trace, when the options ask for one; without them the
// dependencies come back as they were and nothing is created. The
// function it returns builds the traces under way and flushes the
// exporter.
func withTracing(ctx context.Context, deps application.Dependencies, options Options, logger levelLogger) (application.Dependencies, func(), error) {
	exporter := options.TraceExporter
	if exporter == nil {
		if options.OTLPEndpoint == "" {
			return deps, func() {}, nil
		}
		var err error
		if exporter, err = tracing.NewOTLPExporter(ctx, options.OTLPEndpoint); err != nil {
			return deps, nil, fmt.Errorf("daemon: %w", err)
		}
		logger.Printf("daemon: tracing to %s", options.OTLPEndpoint)
	}
	provider := tracing.NewProvider(exporter, options.TraceExporter == nil)
	source := tracing.NewSource(deps.Tasks, deps.Executions, deps.EventReader)
	publisher := tracing.NewPublisher(deps.Events, tracing.NewTaskTrace(source, provider), logger.Printf)
	deps.Events = publisher
	return deps, func() {
		publisher.Wait()
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
		defer cancel()
		if err := provider.Shutdown(flush); err != nil {
			logger.Printf("daemon: tracing: flushing spans: %v", err)
		}
	}, nil
}
