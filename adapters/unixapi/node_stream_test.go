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
		_, _ = client.SubmitTask(context.Background(), SubmitRequest{BotID: "bot-a", Objective: "Patrol", IdempotencyKey: "k-1"})
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

// A stream further behind than the hints kept is told to resync.
func TestChangesKeepTheLastHints(t *testing.T) {
	hub := newChanges()
	signal, seq := hub.next()
	hub.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-signal:
		t.Fatal("a heartbeat woke the waiters")
	default:
	}
	hub.saw(domain.Event{Type: domain.EventTaskPreempted, ExecutionID: "exec-1"})
	<-signal
	hinted, next, complete := hub.since(seq)
	if !complete || len(hinted) != 1 || hinted[0] != "exec-1" {
		t.Fatalf("since = %v %v", hinted, complete)
	}
	if hinted, _, complete := hub.since(next); !complete || len(hinted) != 0 {
		t.Fatalf("nothing new = %v %v", hinted, complete)
	}
	for range maxHints + 1 {
		hub.saw(domain.Event{Type: domain.EventTaskMessage, ExecutionID: "exec-2"})
	}
	if _, _, complete := hub.since(next); complete {
		t.Error("dropped hints not reported")
	}
}
