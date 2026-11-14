package application

import (
	"context"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// maxHints is how many pushes the hub keeps per node for a stream that
// fell behind; one further behind is told to look at all it runs.
const maxHints = 256

// hint is one push kept for a node's stream, in order.
type hint struct {
	seq  uint64
	push NodePush
}

// ChangeHub is the direct bus adapter and what the long polls wait on:
// every event the control plane records is a change that wakes the
// waits on a timeline or a delegation; an event that may give a node
// something to run wakes the waits for an assignment; a push to a node
// is kept for that node's stream, which the node holds open over its own
// connection to the control plane. A node's heartbeat changes nothing a
// waiter looks for.
type ChangeHub struct {
	mu     sync.Mutex
	signal chan struct{}
	work   chan struct{}
	seq    uint64
	hints  map[domain.NodeID][]hint
	// dropped is the sequence of the last push dropped per node.
	dropped map[domain.NodeID]uint64
}

var _ NodePusher = (*ChangeHub)(nil)

// NewChangeHub is an empty hub.
func NewChangeHub() *ChangeHub {
	return &ChangeHub{signal: make(chan struct{}), work: make(chan struct{}), hints: map[domain.NodeID][]hint{}, dropped: map[domain.NodeID]uint64{}}
}

// saw is called with each event recorded.
func (c *ChangeHub) saw(event domain.Event) {
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
func (c *ChangeHub) wake() {
	close(c.signal)
	c.signal = make(chan struct{})
}

// Push keeps the push for the node's stream and wakes it.
func (c *ChangeHub) Push(_ context.Context, node domain.NodeID, push NodePush) error {
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

// Next answers the channel the next change closes and the sequence of
// the last push kept: take both before reading the state, so a change
// while reading is not missed.
func (c *ChangeHub) Next() (<-chan struct{}, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.signal, c.seq
}

// NextWork answers the channel the next change that may give a node
// something to run closes; take it before looking for an assignment.
func (c *ChangeHub) NextWork() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.work
}

// Since answers the node's pushes kept after seq, the sequence to ask
// from next time, and false when a push to the node after seq was
// dropped (the stream fell further behind than the hub keeps).
func (c *ChangeHub) Since(node domain.NodeID, seq uint64) ([]NodePush, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []NodePush
	for _, h := range c.hints[node] {
		if h.seq > seq {
			out = append(out, h.push)
		}
	}
	return out, c.seq, c.dropped[node] <= seq
}

// tappedEvents records events through the inner publisher, tells the hub
// of each one recorded and pushes those about an execution to its node,
// through the direct adapter and every other pusher configured. A task
// cancelled or preempted has its in-process run, if one is going on,
// interrupted through interrupt: StopBot, an urgent submission and a
// team's stop.all end tasks without the API's cancel, and their runs
// went on to their own end (QA 5 Nov 2026: a stopped bot's run kept the
// in-process worker for its whole timeout).
type tappedEvents struct {
	inner      EventPublisher
	changes    *ChangeHub
	executions ExecutionRepository
	pushers    *pushers
	interrupt  func(domain.TaskID) bool
}

// interruptingTypes are the events that end a task's run where it is.
var interruptingTypes = map[domain.EventType]bool{
	domain.EventTaskCancelled: true,
	domain.EventTaskPreempted: true,
}

func (t tappedEvents) Publish(ctx context.Context, event domain.Event) error {
	if err := t.inner.Publish(ctx, event); err != nil {
		return err
	}
	t.changes.saw(event)
	if interruptingTypes[event.Type] && event.TaskID != "" && t.interrupt != nil {
		t.interrupt(event.TaskID)
	}
	if event.ExecutionID == "" || !hintedTypes[event.Type] || t.executions == nil {
		return nil
	}
	execution, err := t.executions.Get(ctx, event.ExecutionID)
	if err != nil || execution.NodeID() == "" {
		return nil
	}
	push := NodePush{Execution: event.ExecutionID}
	_ = t.changes.Push(ctx, execution.NodeID(), push)
	t.pushers.push(ctx, execution.NodeID(), push)
	return nil
}

// pushers are the bus adapters beside the direct one, added as the
// daemon is configured.
type pushers struct {
	mu   sync.Mutex
	list []NodePusher
	log  func(string, ...any)
}

func (p *pushers) add(pusher NodePusher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, pusher)
}

func (p *pushers) push(ctx context.Context, node domain.NodeID, push NodePush) {
	p.mu.Lock()
	list := append([]NodePusher(nil), p.list...)
	p.mu.Unlock()
	for _, pusher := range list {
		if err := pusher.Push(ctx, node, push); err != nil && p.log != nil {
			p.log("bus: push to %s: %v", node, err)
		}
	}
}

// Changed answers the channel the next change the control plane records
// closes: take it before reading the state a long poll or a watch looks
// at, so a change while reading is not missed.
func (l *LocalOperator) Changed() <-chan struct{} {
	changed, _ := l.changes.Next()
	return changed
}
