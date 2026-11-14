package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestActivityChangesWakeOnEveryEventRecorded(t *testing.T) {
	hub := NewChangeHub()
	signal := activityChanges{hub: hub}.Next()
	select {
	case <-signal:
		t.Fatal("woken with nothing recorded")
	default:
	}
	hub.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-signal:
		t.Fatal("a heartbeat is no change")
	default:
	}
	hub.saw(domain.Event{Type: domain.EventTaskProgress})
	select {
	case <-signal:
	default:
		t.Fatal("an event did not wake the watch")
	}
	local := &LocalOperator{changes: hub}
	if local.ActivityChanges() == nil {
		t.Error("no signal")
	}
}

// Changed is taken before reading the state; the change that comes after
// closes it, and a heartbeat does not.
func TestChangedClosesOnTheNextChangeRecorded(t *testing.T) {
	local := &LocalOperator{changes: NewChangeHub()}
	changed := local.Changed()
	select {
	case <-changed:
		t.Fatal("closed with nothing recorded")
	default:
	}
	local.changes.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-changed:
		t.Fatal("a heartbeat is no change")
	default:
	}
	local.changes.saw(domain.Event{Type: domain.EventTaskProgress})
	select {
	case <-changed:
	default:
		t.Fatal("a change did not close the channel")
	}
	select {
	case <-local.Changed():
		t.Fatal("the next channel starts open")
	default:
	}
}

// The hub keeps each node's pushes in order; a stream further behind
// than the pushes kept is told to resync; another node's pushes are not
// its own.
func TestChangesKeepTheLastHints(t *testing.T) {
	ctx := context.Background()
	hub := NewChangeHub()
	signal, seq := hub.Next()
	hub.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-signal:
		t.Fatal("a heartbeat woke the waiters")
	default:
	}
	_ = hub.Push(ctx, "web-1", NodePush{Execution: "exec-1"})
	<-signal
	pushes, next, complete := hub.Since("web-1", seq)
	if !complete || len(pushes) != 1 || pushes[0].Execution != "exec-1" {
		t.Fatalf("since = %v %v", pushes, complete)
	}
	if pushes, _, complete := hub.Since("web-2", seq); !complete || len(pushes) != 0 {
		t.Fatalf("another node = %v %v", pushes, complete)
	}
	if pushes, _, complete := hub.Since("web-1", next); !complete || len(pushes) != 0 {
		t.Fatalf("nothing new = %v %v", pushes, complete)
	}
	for range maxHints + 1 {
		_ = hub.Push(ctx, "web-1", NodePush{Execution: "exec-2"})
	}
	if pushes, _, complete := hub.Since("web-1", next); complete || len(pushes) != maxHints {
		t.Errorf("dropped pushes not reported: %d %v", len(pushes), complete)
	}
	if _, _, complete := hub.Since("web-2", next); !complete {
		t.Error("another node's drops reported")
	}
}
