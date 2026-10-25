package nodeagent

import (
	"context"
	"sync"
)

// slots counts the executions that are working. The agent takes an
// assignment only while fewer than the limit work; an execution that
// waits for a child it delegated lends its slot, so the child can run on
// the same node, and takes it back without waiting when the wait ends
// (waiting there could hold a parent behind its own child's sibling).
type slots struct {
	mu      sync.Mutex
	changed chan struct{}
	limit   int
	working int
}

func newSlots(limit int) *slots {
	if limit < 1 {
		limit = 1
	}
	return &slots{limit: limit, changed: make(chan struct{})}
}

// acquire waits for a free slot, or for ctx; false when ctx ended.
func (s *slots) acquire(ctx context.Context) bool {
	for {
		s.mu.Lock()
		if s.working < s.limit {
			s.working++
			s.mu.Unlock()
			return true
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-changed:
		}
	}
}

// release frees a slot an execution took.
func (s *slots) release() { s.add(-1) }

// lend frees the slot of an execution that waits for a child.
func (s *slots) lend() { s.add(-1) }

// reclaim takes the slot back after the wait, even over the limit.
func (s *slots) reclaim() { s.add(1) }

func (s *slots) add(delta int) {
	s.mu.Lock()
	s.working += delta
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

// busy is how many executions work now.
func (s *slots) busy() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.working
}
