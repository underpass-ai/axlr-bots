package unixapi

import (
	"context"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// maxHints is how many pushes the hub keeps per node for a stream that
// fell behind; one further behind is told to look at all it runs.
const maxHints = 256

// hint is one push kept for a node's stream, in order.
type hint struct {
	seq  uint64
	push application.NodePush
}

// changes is the direct bus adapter and what the long polls wait on:
// every event the control plane records is a change that wakes the
// waits on a timeline or a delegation; an event that may give a node
// something to run wakes the waits for an assignment; a push to a node
// is kept for that node's stream, which the node holds open over its own
// connection to the control plane. A node's heartbeat changes nothing a
// waiter looks for.
type changes struct {
	mu     sync.Mutex
	signal chan struct{}
	work   chan struct{}
	seq    uint64
	hints  map[domain.NodeID][]hint
	// dropped is the sequence of the last push dropped per node.
	dropped map[domain.NodeID]uint64
}

var _ application.NodePusher = (*changes)(nil)

func newChanges() *changes {
	return &changes{signal: make(chan struct{}), work: make(chan struct{}), hints: map[domain.NodeID][]hint{}, dropped: map[domain.NodeID]uint64{}}
}

// saw is called with each event recorded.
func (c *changes) saw(event domain.Event) {
	if event.Type == domain.EventNodeHeartbeat {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wake()
	if workTypes[event.Type] {
		close(c.work)
		c.work = make(chan struct{})
	}
}

// wake closes the signal; the caller holds the lock.
func (c *changes) wake() {
	close(c.signal)
	c.signal = make(chan struct{})
}

// Push keeps the push for the node's stream and wakes it.
func (c *changes) Push(_ context.Context, node domain.NodeID, push application.NodePush) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	kept := append(c.hints[node], hint{seq: c.seq, push: push})
	if len(kept) > maxHints {
		c.dropped[node] = kept[len(kept)-maxHints-1].seq
		kept = kept[len(kept)-maxHints:]
	}
	c.hints[node] = kept
	c.wake()
	return nil
}

// hintedTypes are the events about an execution its node acts on at once.
var hintedTypes = map[domain.EventType]bool{
	domain.EventTaskCancelled: true,
	domain.EventTaskPreempted: true,
	domain.EventTaskExpired:   true,
	domain.EventTaskMessage:   true,
}

// workTypes are the events after which a node may have something new to
// run: a task created, a decision queued, a bot that may now take its
// tasks or be placed elsewhere, a node that may now take them.
var workTypes = map[domain.EventType]bool{
	domain.EventTaskSubmitted:   true,
	domain.EventTriggerFired:    true,
	domain.EventTaskDecided:     true,
	domain.EventApprovalExpired: true,
	domain.EventBotEnabled:      true,
	domain.EventBotUpdated:      true,
	domain.EventNodeRegistered:  true,
	domain.EventTaskDelegated:   true,
	domain.EventEventHeard:      true,
}

// next answers the channel the next change closes and the sequence of
// the last push kept: take both before reading the state, so a change
// while reading is not missed.
func (c *changes) next() (<-chan struct{}, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.signal, c.seq
}

// nextWork answers the channel the next change that may give a node
// something to run closes; take it before looking for an assignment.
func (c *changes) nextWork() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.work
}

// since answers the node's pushes kept after seq, the sequence to ask
// from next time, and false when a push to the node after seq was
// dropped (the stream fell further behind than the hub keeps).
func (c *changes) since(node domain.NodeID, seq uint64) ([]application.NodePush, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []application.NodePush
	for _, h := range c.hints[node] {
		if h.seq > seq {
			out = append(out, h.push)
		}
	}
	return out, c.seq, c.dropped[node] <= seq
}

// awaitChange returns when signal closes, after d, or when ctx ends;
// false when ctx ended.
func awaitChange(ctx context.Context, signal <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-signal:
	case <-timer.C:
	}
	return true
}

// tappedEvents records events through the inner publisher, tells the hub
// of each one recorded and pushes those about an execution to its node,
// through the direct adapter and every other pusher configured.
type tappedEvents struct {
	inner      application.EventPublisher
	changes    *changes
	executions application.ExecutionRepository
	pushers    *pushers
}

func (t tappedEvents) Publish(ctx context.Context, event domain.Event) error {
	if err := t.inner.Publish(ctx, event); err != nil {
		return err
	}
	t.changes.saw(event)
	if event.ExecutionID == "" || !hintedTypes[event.Type] || t.executions == nil {
		return nil
	}
	execution, err := t.executions.Get(ctx, event.ExecutionID)
	if err != nil || execution.NodeID() == "" {
		return nil
	}
	push := application.NodePush{Execution: event.ExecutionID}
	_ = t.changes.Push(ctx, execution.NodeID(), push)
	t.pushers.push(ctx, execution.NodeID(), push)
	return nil
}

// pushers are the bus adapters beside the direct one, added as the
// daemon is configured.
type pushers struct {
	mu   sync.Mutex
	list []application.NodePusher
	log  func(string, ...any)
}

func (p *pushers) add(pusher application.NodePusher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, pusher)
}

func (p *pushers) push(ctx context.Context, node domain.NodeID, push application.NodePush) {
	p.mu.Lock()
	list := append([]application.NodePusher(nil), p.list...)
	p.mu.Unlock()
	for _, pusher := range list {
		if err := pusher.Push(ctx, node, push); err != nil && p.log != nil {
			p.log("bus: push to %s: %v", node, err)
		}
	}
}
