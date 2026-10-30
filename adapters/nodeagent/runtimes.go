package nodeagent

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Hearer is a runtime that takes the operator's and the teammates'
// messages for a running execution as they were published (the event's
// name and data), not as text: a Jev program reads a cue's data.
type Hearer interface {
	Hear(ctx context.Context, execution domain.ExecutionID, messages []application.RelayedMessage) error
}

// Runtimes is the node's executor over several runtimes: AXLR (the
// default, which also decides, opens what was delivered and names the
// node's capabilities) and the others by kind. A run is dispatched on
// its request's runtime; a decision is always AXLR's.
type Runtimes struct {
	Default Executor
	// Others are the runtimes beside AXLR, by kind.
	Others map[domain.RuntimeKind]application.RuntimeExecutor

	mu      sync.Mutex
	running map[domain.ExecutionID]domain.RuntimeKind
}

// NewRuntimes wires the router over the default executor.
func NewRuntimes(axlr Executor) *Runtimes {
	return &Runtimes{Default: axlr, Others: map[domain.RuntimeKind]application.RuntimeExecutor{}, running: map[domain.ExecutionID]domain.RuntimeKind{}}
}

// Add registers a runtime beside AXLR.
func (r *Runtimes) Add(kind domain.RuntimeKind, executor application.RuntimeExecutor) {
	r.Others[kind] = executor
}

// Kinds names the runtimes beside AXLR, for the node's capabilities.
func (r *Runtimes) Kinds() []string {
	var kinds []string
	for kind := range r.Others {
		kinds = append(kinds, kind.String())
	}
	return kinds
}

// Execute runs the request on its runtime.
func (r *Runtimes) Execute(ctx context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	kind := request.Runtime.Kind()
	if kind == domain.RuntimeAXLR {
		return r.Default.Execute(ctx, request)
	}
	executor, ok := r.Others[kind]
	if !ok {
		return application.ExecutionReport{}, errors.New("this node has no " + kind.String() + " runtime")
	}
	r.mu.Lock()
	r.running[request.ExecutionID] = kind
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.running, request.ExecutionID)
		r.mu.Unlock()
	}()
	return executor.Execute(ctx, request)
}

// Decide is AXLR's: no other runtime stops on a pending call.
func (r *Runtimes) Decide(ctx context.Context, request application.DecisionRequest) (application.ExecutionReport, error) {
	return r.Default.Decide(ctx, request)
}

// Hear hands the messages to the execution's runtime: as events to one
// that hears them, as text to AXLR's steer file.
func (r *Runtimes) Hear(ctx context.Context, execution domain.ExecutionID, messages []application.RelayedMessage) error {
	r.mu.Lock()
	kind, ok := r.running[execution]
	r.mu.Unlock()
	if ok {
		if hearer, ok := r.Others[kind].(Hearer); ok {
			return hearer.Hear(ctx, execution, messages)
		}
		return errors.New("the " + kind.String() + " runtime takes no messages")
	}
	steerer, ok := r.Default.(Steerer)
	if !ok {
		return errors.New("the runtime takes no messages")
	}
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		texts = append(texts, message.Text)
	}
	return steerer.Steer(ctx, execution, texts)
}

func (r *Runtimes) OpenDelivered(bot domain.BotID, reference string) (io.ReadCloser, error) {
	return r.Default.OpenDelivered(bot, reference)
}

func (r *Runtimes) SupportsPolicy(ctx context.Context) bool { return r.Default.SupportsPolicy(ctx) }
func (r *Runtimes) SupportsCorrelation(ctx context.Context) bool {
	return r.Default.SupportsCorrelation(ctx)
}
func (r *Runtimes) Version(ctx context.Context) string { return r.Default.Version(ctx) }
