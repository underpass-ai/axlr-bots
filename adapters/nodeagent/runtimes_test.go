package nodeagent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// recordingExecutor is a runtime that records what it was handed.
type recordingExecutor struct {
	ran     []domain.ExecutionID
	heard   []application.RelayedMessage
	steers  []string
	started chan struct{}
	block   chan struct{}
}

func (r *recordingExecutor) Execute(_ context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	r.ran = append(r.ran, request.ExecutionID)
	if r.started != nil {
		close(r.started)
	}
	if r.block != nil {
		<-r.block
	}
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted}, nil
}

func (r *recordingExecutor) Decide(context.Context, application.DecisionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "decided"}}, nil
}

func (r *recordingExecutor) Hear(_ context.Context, _ domain.ExecutionID, messages []application.RelayedMessage) error {
	r.heard = append(r.heard, messages...)
	return nil
}

func (r *recordingExecutor) Steer(_ context.Context, _ domain.ExecutionID, texts []string) error {
	r.steers = append(r.steers, texts...)
	return nil
}

func (r *recordingExecutor) OpenDelivered(domain.BotID, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("delivered")), nil
}
func (r *recordingExecutor) SupportsPolicy(context.Context) bool      { return true }
func (r *recordingExecutor) SupportsCorrelation(context.Context) bool { return false }
func (r *recordingExecutor) Version(context.Context) string           { return "fake 1" }

// A run goes to its runtime; a decision, the workspace and the
// capabilities are AXLR's; a message reaches the runtime of the execution
// that runs, as an event for one that hears, as text for AXLR.
func TestRuntimesDispatchOnTheRequestsRuntime(t *testing.T) {
	ctx := context.Background()
	axlr := &recordingExecutor{}
	jev := &recordingExecutor{block: make(chan struct{}), started: make(chan struct{})}
	runtimes := NewRuntimes(axlr)
	runtimes.Add(domain.RuntimeJev, jev)
	if kinds := runtimes.Kinds(); len(kinds) != 1 || kinds[0] != "jev" {
		t.Errorf("kinds = %v", kinds)
	}
	if _, err := runtimes.Execute(ctx, application.ExecutionRequest{ExecutionID: "exec-a"}); err != nil || len(axlr.ran) != 1 {
		t.Fatalf("axlr run: %v %v", err, axlr.ran)
	}
	program, _ := domain.NewRuntime(&domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{Server: "s", Start: "a", Wait: "w", Act: domain.JevActSnapshot{Tool: "b", Choice: "c"}}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := runtimes.Execute(ctx, application.ExecutionRequest{ExecutionID: "exec-j", Runtime: program}); err != nil {
			t.Error(err)
		}
	}()
	<-jev.started
	message := application.RelayedMessage{ID: "evt-1", Text: "Your operator says (info.notice.load): high", Event: &application.RelayedEvent{Name: "info.notice.load", Data: map[string]any{"priority": "high"}}}
	if err := runtimes.Hear(ctx, "exec-j", []application.RelayedMessage{message}); err != nil || len(jev.heard) != 1 || jev.heard[0].Event.Name != "info.notice.load" {
		t.Errorf("jev heard: %v %v", err, jev.heard)
	}
	if err := runtimes.Hear(ctx, "exec-a", []application.RelayedMessage{message}); err != nil || len(axlr.steers) != 1 || axlr.steers[0] != message.Text {
		t.Errorf("axlr steered: %v %v", err, axlr.steers)
	}
	close(jev.block)
	<-done
	// Once the run ended, the message goes to AXLR's steer as before.
	if err := runtimes.Hear(ctx, "exec-j", []application.RelayedMessage{message}); err != nil || len(axlr.steers) != 2 {
		t.Errorf("after the run: %v %d", err, len(axlr.steers))
	}
	unknown, _ := domain.NewRuntime(&domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{Server: "s", Start: "a", Wait: "w", Act: domain.JevActSnapshot{Tool: "b", Choice: "c"}}})
	bare := NewRuntimes(axlr)
	if _, err := bare.Execute(ctx, application.ExecutionRequest{ExecutionID: "exec-x", Runtime: unknown}); err == nil || !strings.Contains(err.Error(), "no jev runtime") {
		t.Errorf("a runtime the node lacks: %v", err)
	}
	if report, err := runtimes.Decide(ctx, application.DecisionRequest{}); err != nil || report.Result.Answer != "decided" {
		t.Errorf("decide: %v %+v", err, report)
	}
	file, err := runtimes.OpenDelivered("bot", "x")
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := io.ReadAll(file); string(raw) != "delivered" || !runtimes.SupportsPolicy(ctx) || runtimes.SupportsCorrelation(ctx) || runtimes.Version(ctx) != "fake 1" {
		t.Error("the default's workspace and capabilities")
	}
}

func TestRuntimesWithoutASteererRefuseMessages(t *testing.T) {
	runtimes := &Runtimes{Default: deafOnly{}, Others: map[domain.RuntimeKind]application.RuntimeExecutor{domain.RuntimeJev: &recordingExecutor{}}, running: map[domain.ExecutionID]domain.RuntimeKind{"exec-j": domain.RuntimeJev}}
	if err := runtimes.Hear(context.Background(), "exec-a", nil); err == nil || !strings.Contains(err.Error(), "takes no messages") {
		t.Errorf("no steerer: %v", err)
	}
	runtimes.Others[domain.RuntimeJev] = silentRuntime{}
	if err := runtimes.Hear(context.Background(), "exec-j", nil); err == nil || !strings.Contains(err.Error(), "jev runtime takes no messages") {
		t.Errorf("a runtime that does not hear: %v", err)
	}
}

// deafOnly is an Executor without Steer.
type deafOnly struct{}

func (deafOnly) Execute(context.Context, application.ExecutionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{}, nil
}
func (deafOnly) Decide(context.Context, application.DecisionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{}, nil
}
func (deafOnly) OpenDelivered(domain.BotID, string) (io.ReadCloser, error) {
	return nil, errors.New("none")
}
func (deafOnly) SupportsPolicy(context.Context) bool      { return false }
func (deafOnly) SupportsCorrelation(context.Context) bool { return false }
func (deafOnly) Version(context.Context) string           { return "" }

// silentRuntime runs but does not hear.
type silentRuntime struct{}

func (silentRuntime) Execute(context.Context, application.ExecutionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{}, nil
}
