package unixapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A long poll is answered as soon as the task it waits for is created,
// not at its next look; the node's stream opens with a resync and pushes
// the cancel of an execution the node runs, and nothing about another
// node's.
func TestTheControlPlanePushesToItsNodes(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, id := range []string{"web-1", "web-2"} {
		if _, err := client.Register(ctx, application.NodeRegistration{ID: id, HostName: id, Capabilities: domain.NodeCapabilities{AXLRVersion: "dev", PolicySupported: true}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.RegisterBot(ctx, definition("bot-a", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	pushes, err := client.Stream(ctx, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if push := <-pushes; !push.All {
		t.Fatalf("the stream opened with %+v, not a resync", push)
	}
	others, err := client.Stream(ctx, "web-2")
	if err != nil {
		t.Fatal(err)
	}
	<-others
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = client.SubmitTask(context.Background(), SubmitRequest{BotID: "bot-a", Objective: "Watch", IdempotencyKey: "k-1"})
	}()
	started := time.Now()
	assignment, found, err := client.NextAssignment(ctx, "web-1", 20*time.Second)
	if err != nil || !found || time.Since(started) > recheckEvery/2 {
		t.Fatalf("the long poll answered after %s: %v %v", time.Since(started), err, found)
	}
	if _, err := client.CancelTask(ctx, assignment.Run.TaskID.String()); err != nil {
		t.Fatal(err)
	}
	select {
	case push := <-pushes:
		if push.All || push.Execution != assignment.ExecutionID {
			t.Errorf("pushed %+v, want the cancelled execution", push)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel was not pushed")
	}
	select {
	case push := <-others:
		t.Errorf("another node heard %+v", push)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := client.Stream(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("a stream for an unknown node: %v", err)
	}
	// The stream ends when its context does.
	cancel()
	for range pushes {
	}
}

// A control plane without the route answers the mux's own 404: the node
// hears ErrNoStream and renews on its clock.
func TestAnOlderControlPlaneServesNoStream(t *testing.T) {
	server := httptest.NewServer(http.NewServeMux())
	defer server.Close()
	if _, err := NewClient(server.URL).Stream(context.Background(), "web-1"); !errors.Is(err, application.ErrNoStream) {
		t.Errorf("err = %v", err)
	}
}

// The hub keeps each node's pushes in order; a stream further behind
// than the pushes kept is told to resync; another node's pushes are not
// its own.
func TestChangesKeepTheLastHints(t *testing.T) {
	ctx := context.Background()
	hub := newChanges()
	signal, seq := hub.next()
	hub.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-signal:
		t.Fatal("a heartbeat woke the waiters")
	default:
	}
	_ = hub.Push(ctx, "web-1", application.NodePush{Execution: "exec-1"})
	<-signal
	pushes, next, complete := hub.since("web-1", seq)
	if !complete || len(pushes) != 1 || pushes[0].Execution != "exec-1" {
		t.Fatalf("since = %v %v", pushes, complete)
	}
	if pushes, _, complete := hub.since("web-2", seq); !complete || len(pushes) != 0 {
		t.Fatalf("another node = %v %v", pushes, complete)
	}
	if pushes, _, complete := hub.since("web-1", next); !complete || len(pushes) != 0 {
		t.Fatalf("nothing new = %v %v", pushes, complete)
	}
	for range maxHints + 1 {
		_ = hub.Push(ctx, "web-1", application.NodePush{Execution: "exec-2"})
	}
	if pushes, _, complete := hub.since("web-1", next); complete || len(pushes) != maxHints {
		t.Errorf("dropped pushes not reported: %d %v", len(pushes), complete)
	}
	if _, _, complete := hub.since("web-2", next); !complete {
		t.Error("another node's drops reported")
	}
}
