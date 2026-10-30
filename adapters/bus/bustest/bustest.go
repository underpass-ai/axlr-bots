// Package bustest is the bus port's contract: what every adapter must do
// for the control plane's pushes to reach a node. Each adapter's test
// runs it with its own factory.
package bustest

import (
	"context"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Open opens a node's stream, as the agent does.
type Open func(ctx context.Context, node domain.NodeID) (<-chan application.NodePush, error)

// Factory builds one adapter's two ends over nodes NodeA and NodeB.
type Factory func(t *testing.T) (application.NodePusher, Open)

// The nodes the contract pushes to.
const (
	NodeA domain.NodeID = "bus-a"
	NodeB domain.NodeID = "bus-b"
)

// Run checks the adapter against the contract: a stream opens with a
// resync; pushes reach their node in order and no other node; a push
// repeated is tolerated; a stream opened again resyncs first; every
// push's Ack, when set, may be called.
func Run(t *testing.T, factory Factory) {
	t.Run("a stream opens with a resync and takes the pushes in order", func(t *testing.T) {
		pusher, open := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := opened(t, open, ctx, NodeA)
		for _, execution := range []domain.ExecutionID{"exec-1", "exec-2", "exec-3"} {
			if err := pusher.Push(ctx, NodeA, application.NodePush{Execution: execution}); err != nil {
				t.Fatal(err)
			}
		}
		for _, want := range []domain.ExecutionID{"exec-1", "exec-2", "exec-3"} {
			if got := nextExecution(t, a); got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		}
	})
	t.Run("another node hears nothing of it", func(t *testing.T) {
		pusher, open := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a, b := opened(t, open, ctx, NodeA), opened(t, open, ctx, NodeB)
		_ = pusher.Push(ctx, NodeA, application.NodePush{Execution: "exec-1"})
		if got := nextExecution(t, a); got != "exec-1" {
			t.Fatalf("a got %s", got)
		}
		if push, ok := receive(b, 300*time.Millisecond); ok {
			t.Fatalf("b heard %+v", push)
		}
	})
	t.Run("a push repeated is tolerated", func(t *testing.T) {
		pusher, open := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := opened(t, open, ctx, NodeA)
		_ = pusher.Push(ctx, NodeA, application.NodePush{Execution: "exec-4"})
		_ = pusher.Push(ctx, NodeA, application.NodePush{Execution: "exec-4"})
		_ = pusher.Push(ctx, NodeA, application.NodePush{Execution: "exec-5"})
		seen := 0
		for {
			got := nextExecution(t, a)
			if got == "exec-4" {
				seen++
				continue
			}
			if got != "exec-5" || seen < 1 || seen > 2 {
				t.Fatalf("got %s after %d exec-4", got, seen)
			}
			break
		}
	})
	t.Run("a stream opened again resyncs first", func(t *testing.T) {
		pusher, open := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		first, stop := context.WithCancel(ctx)
		a := opened(t, open, first, NodeA)
		stop()
		for range a {
		}
		_ = pusher.Push(ctx, NodeA, application.NodePush{Execution: "exec-6"})
		again, err := open(ctx, NodeA)
		if err != nil {
			t.Fatal(err)
		}
		push, ok := receive(again, 5*time.Second)
		if !ok || !push.All {
			t.Fatalf("first push after reopening = %+v %v", push, ok)
		}
		if push.Ack != nil {
			push.Ack()
		}
	})
}

// opened opens the stream and takes its opening resync.
func opened(t *testing.T, open Open, ctx context.Context, node domain.NodeID) <-chan application.NodePush {
	t.Helper()
	stream, err := open(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	push, ok := receive(stream, 5*time.Second)
	if !ok || !push.All {
		t.Fatalf("%s: the stream did not open with a resync: %+v %v", node, push, ok)
	}
	if push.Ack != nil {
		push.Ack()
	}
	return stream
}

// nextExecution waits for the next push about an execution, skipping
// resyncs, and acknowledges it.
func nextExecution(t *testing.T, stream <-chan application.NodePush) domain.ExecutionID {
	t.Helper()
	for {
		push, ok := receive(stream, 5*time.Second)
		if !ok {
			t.Fatal("no push came")
		}
		if push.Ack != nil {
			push.Ack()
		}
		if push.All {
			continue
		}
		return push.Execution
	}
}

func receive(stream <-chan application.NodePush, timeout time.Duration) (application.NodePush, bool) {
	select {
	case push, ok := <-stream:
		return push, ok
	case <-time.After(timeout):
		return application.NodePush{}, false
	}
}
