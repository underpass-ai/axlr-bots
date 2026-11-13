package metrics

import (
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ledgerCapacity bounds the executions the ledger remembers. A
// forgotten execution that is reported again is counted again, which
// takes a report that arrives after thousands of others.
const ledgerCapacity = 8192

// executionLedger remembers which ended executions the metrics have
// counted, so the several events one execution leaves (its outcome, its
// report) count it once, and which of them ended with a cost nobody
// knew, so a late report that tells it is counted when it comes. It
// forgets the oldest past its capacity.
type executionLedger struct {
	mu      sync.Mutex
	entries map[domain.ExecutionID]bool // true: counted with an unknown cost
	order   []domain.ExecutionID
}

func newExecutionLedger() *executionLedger {
	return &executionLedger{entries: map[domain.ExecutionID]bool{}}
}

// count claims the execution; false when it was counted already.
func (l *executionLedger) count(id domain.ExecutionID, costUnknown bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, seen := l.entries[id]; seen {
		return false
	}
	if len(l.order) >= ledgerCapacity {
		delete(l.entries, l.order[0])
		l.order = l.order[1:]
	}
	l.entries[id] = costUnknown
	l.order = append(l.order, id)
	return true
}

// state says whether the execution was counted, and with an unknown
// cost.
func (l *executionLedger) state(id domain.ExecutionID) (counted, costUnknown bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	costUnknown, counted = l.entries[id]
	return counted, costUnknown
}

// settle claims the cost a late report gave; false when it was claimed
// already or the execution was not waiting for one.
func (l *executionLedger) settle(id domain.ExecutionID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.entries[id] {
		return false
	}
	l.entries[id] = false
	return true
}

// unknown lists the executions still waiting for their cost.
func (l *executionLedger) unknown() []domain.ExecutionID {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ids []domain.ExecutionID
	for _, id := range l.order {
		if l.entries[id] {
			ids = append(ids, id)
		}
	}
	return ids
}
