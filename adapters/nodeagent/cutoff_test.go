package nodeagent

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var errRevoked = domain.ForbiddenError{Reason: "node web-1 was revoked: enroll it again with a fresh token", Revoked: true}

// revokingTransport refuses the node as revoked from a given call on,
// and counts what the node still asks after that.
type revokingTransport struct {
	fakeTransport
	mu              sync.Mutex
	revokeRegister  bool
	beats, polls    int
	revokedAt       chan struct{}
	afterRevocation int
	revoked         bool
}

func (r *revokingTransport) Register(context.Context, application.NodeRegistration) (domain.NodeSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revokeRegister {
		r.revoke()
		return domain.NodeSnapshot{}, errRevoked
	}
	return domain.NodeSnapshot{}, nil
}

// Heartbeat answers the first beat and refuses the second as revoked.
func (r *revokingTransport) Heartbeat(context.Context, domain.NodeID) (domain.NodeSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beats++
	if r.revoked {
		r.afterRevocation++
		return domain.NodeSnapshot{}, errRevoked
	}
	if r.beats >= 2 {
		r.revoke()
		return domain.NodeSnapshot{}, errRevoked
	}
	return domain.NodeSnapshot{}, nil
}

func (r *revokingTransport) NextAssignment(ctx context.Context, _ domain.NodeID, wait time.Duration) (application.Assignment, bool, error) {
	r.mu.Lock()
	r.polls++
	if r.revoked {
		r.afterRevocation++
		r.mu.Unlock()
		return application.Assignment{}, false, errRevoked
	}
	r.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
	return application.Assignment{}, false, nil
}

func (r *revokingTransport) revoke() {
	if !r.revoked {
		r.revoked = true
		close(r.revokedAt)
	}
}

func (r *revokingTransport) counts() (beats, polls, after int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beats, r.polls, r.afterRevocation
}

func revokedLines(logs string) int {
	return strings.Count(logs, "is revoked: the control plane refuses it for good")
}

// A node revoked while it runs stops heartbeating and polling at the
// first refusal, says once what to do, and idles until it is stopped.
func TestAgentRevokedWhileRunningStopsItsLoopsAndSaysSoOnce(t *testing.T) {
	transport := &revokingTransport{revokedAt: make(chan struct{})}
	logs := &syncBuffer{}
	agent := Agent{Transport: transport, Executor: &recordingExecutor{}, Options: Options{NodeID: "web-1", Heartbeat: 5 * time.Millisecond, PollWait: 5 * time.Millisecond, Log: log.New(logs, "", 0)}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	select {
	case <-transport.revokedAt:
	case <-time.After(5 * time.Second):
		t.Fatalf("never revoked:\n%s", logs.String())
	}
	// Many heartbeat and poll intervals: none goes out any more.
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Run returned before it was stopped: %v", err)
	default:
	}
	beats, polls, after := transport.counts()
	if beats < 2 || after > 1 {
		t.Errorf("beats = %d, polls = %d, calls after the revocation = %d:\n%s", beats, polls, after, logs.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run = %v", err)
	}
	if got := revokedLines(logs.String()); got != 1 || !strings.Contains(logs.String(), "axlr-bots node enroll-token --node-id web-1") {
		t.Errorf("%d revoked lines:\n%s", got, logs.String())
	}
	if strings.Contains(logs.String(), "node: heartbeat:") {
		t.Errorf("the refused heartbeat is logged as a passing failure:\n%s", logs.String())
	}
}

// A node that starts revoked does not retry its registration, heartbeat
// or poll.
func TestAgentStartedRevokedAsksNothing(t *testing.T) {
	transport := &revokingTransport{revokedAt: make(chan struct{}), revokeRegister: true}
	logs := &syncBuffer{}
	agent := Agent{Transport: transport, Executor: &recordingExecutor{}, Options: Options{NodeID: "web-1", Heartbeat: 5 * time.Millisecond, PollWait: 5 * time.Millisecond, Log: log.New(logs, "", 0)}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := agent.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if beats, polls, _ := transport.counts(); beats != 0 || polls != 0 {
		t.Errorf("beats = %d, polls = %d", beats, polls)
	}
	if got := revokedLines(logs.String()); got != 1 || strings.Contains(logs.String(), "retrying") {
		t.Errorf("%d revoked lines:\n%s", got, logs.String())
	}
}

// A report refused as revoked is not retried; one refused for another
// reason is.
func TestAgentDoesNotRetryAReportOfARevokedNode(t *testing.T) {
	transport := &reportRefusing{err: errRevoked}
	agent := Agent{Transport: transport, Options: Options{NodeID: "web-1", Log: log.New(&syncBuffer{}, "", 0)}}
	agent.report("web-1", "exec-1", application.ExecutionReport{Outcome: domain.ExecutionFailed})
	if transport.reports != 1 {
		t.Errorf("reports = %d, want 1", transport.reports)
	}
}

type reportRefusing struct {
	fakeTransport
	err error
}

func (r *reportRefusing) Report(context.Context, domain.NodeID, domain.ExecutionID, application.ExecutionReport) (application.ReportAck, error) {
	r.reports++
	return application.ReportAck{}, r.err
}

// Only a refusal that says revoked cuts the node off: a superseded
// certificate's (a renewal under way) or a network failure does not.
func TestCutoffOnlyForRevoked(t *testing.T) {
	stopped := false
	c := newCutoff("web-1", log.New(&syncBuffer{}, "", 0), func() { stopped = true })
	for _, err := range []error{errors.New("connection refused"), domain.ForbiddenError{Reason: "the certificate is not the current one of node web-1"}, nil} {
		if c.revoked(err) || c.happened() || stopped {
			t.Errorf("cut off by %v", err)
		}
	}
	if !c.revoked(errRevoked) || !c.happened() || !stopped {
		t.Error("not cut off when revoked")
	}
	var none *cutoff
	if !none.revoked(errRevoked) || none.happened() {
		t.Error("a nil cutoff")
	}
}
