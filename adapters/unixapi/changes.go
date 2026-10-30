package unixapi

import (
	"context"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// maxHints is how many execution hints the hub keeps for a stream that
// fell behind; one further behind is told to look at all it runs.
const maxHints = 256

// executionHint says that something happened to an execution a node
// runs that the node acts on: it was cancelled or preempted, it expired,
// or a message reached it.
type executionHint struct {
	seq       uint64
	execution domain.ExecutionID
}

// changes is what the long polls and the node streams wait on instead
// of reading the state again every few hundred milliseconds. Every event
// the control plane records (a task created, a decision queued, a task
// cancelled, a message for a run, an event on a team) is a change and
// wakes the waits on a timeline or a delegation; only an event that may
// give a node something to run (a task submitted, a decision queued, a
// bot enabled) wakes the waits for an assignment, so handing one out
// does not wake every node to look again. A node's heartbeat changes
// nothing a waiter looks for.
type changes struct {
	mu     sync.Mutex
	signal chan struct{}
	work   chan struct{}
	seq    uint64
	hints  []executionHint
}

func newChanges() *changes {
	return &changes{signal: make(chan struct{}), work: make(chan struct{})}
}

// saw is called with each event recorded.
func (c *changes) saw(event domain.Event) {
	if event.Type == domain.EventNodeHeartbeat {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if event.ExecutionID != "" && hintedTypes[event.Type] {
		c.seq++
		c.hints = append(c.hints, executionHint{seq: c.seq, execution: event.ExecutionID})
		if len(c.hints) > maxHints {
			c.hints = c.hints[len(c.hints)-maxHints:]
		}
	}
	close(c.signal)
	c.signal = make(chan struct{})
	if workTypes[event.Type] {
		close(c.work)
		c.work = make(chan struct{})
	}
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

// hintedTypes are the events about an execution its node acts on at once.
var hintedTypes = map[domain.EventType]bool{
	domain.EventTaskCancelled: true,
	domain.EventTaskPreempted: true,
	domain.EventTaskExpired:   true,
	domain.EventTaskMessage:   true,
}

// next answers the channel the next change closes and the last hint's
// sequence: take both before reading the state, so a change while
// reading is not missed.
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

// since answers the executions hinted after seq, the sequence to ask
// from next time, and false when hints were dropped since (the caller
// fell further behind than the hub keeps).
func (c *changes) since(seq uint64) ([]domain.ExecutionID, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.hints) == 0 || seq >= c.seq {
		return nil, c.seq, true
	}
	complete := c.hints[0].seq <= seq+1
	var out []domain.ExecutionID
	for _, hint := range c.hints {
		if hint.seq > seq {
			out = append(out, hint.execution)
		}
	}
	return out, c.seq, complete
}

// awaitChange returns when signal closes, after d, or when ctx ends; false
// when ctx ended.
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

// tappedEvents records events through the inner publisher and tells the
// hub of each one recorded.
type tappedEvents struct {
	inner   application.EventPublisher
	changes *changes
}

func (t tappedEvents) Publish(ctx context.Context, event domain.Event) error {
	if err := t.inner.Publish(ctx, event); err != nil {
		return err
	}
	t.changes.saw(event)
	return nil
}
