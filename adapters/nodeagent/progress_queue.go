package nodeagent

import (
	"sync"
	"time"
	"unicode/utf8"

	"github.com/underpass-ai/axlr-bots/application"
)

// progressQueue holds what a run did until the sender hands it to the
// control plane. A push never blocks the run: the model's words, or its
// reasoning, join the item of the same kind at the queue's end, and past the queue's limit the oldest
// item is dropped and counted.
type progressQueue struct {
	mu      sync.Mutex
	items   []application.ProgressItem
	limit   int
	dropped int
	// text counts the characters of words and reasoning waiting; textAt
	// is when the oldest of them came.
	text   int
	textAt time.Time
	// closed: the control plane keeps no more for the execution.
	closed bool
	wake   chan struct{}
}

func newProgressQueue(limit int) *progressQueue {
	return &progressQueue{limit: max(limit, 1), wake: make(chan struct{}, 1)}
}

// push queues one item.
func (q *progressQueue) push(item application.ProgressItem, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if item.Kind.Streamed() {
		if q.text == 0 {
			q.textAt = now
		}
		q.text += utf8.RuneCountInString(item.Text)
		last := len(q.items) - 1
		if last >= 0 && q.items[last].Kind == item.Kind && utf8.RuneCountInString(q.items[last].Text)+utf8.RuneCountInString(item.Text) <= application.MaxProgressText {
			q.items[last].Text += item.Text
			q.signal()
			return
		}
	}
	q.items = append(q.items, item)
	if len(q.items) > q.limit {
		if q.items[0].Kind.Streamed() {
			q.text -= utf8.RuneCountInString(q.items[0].Text)
		}
		q.items = append(q.items[:0:0], q.items[1:]...)
		q.dropped++
	}
	q.signal()
}

// signal wakes the sender; the caller holds the lock.
func (q *progressQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// due says whether a batch should go now: at once for anything but
// words and reasoning, for those once enough of it waits or it has waited long enough.
// When not, it says how long until the waiting text is due (zero: wait
// for the next push).
func (q *progressQueue) due(now time.Time, every time.Duration, chars int) (bool, time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || (len(q.items) == 0 && q.dropped == 0) {
		return false, 0
	}
	for _, item := range q.items {
		if !item.Kind.Streamed() {
			return true, 0
		}
	}
	if q.dropped > 0 || q.text >= chars {
		return true, 0
	}
	waited := now.Sub(q.textAt)
	if waited >= every {
		return true, 0
	}
	return false, every - waited
}

// take empties the queue into a batch, at most a batch's items.
func (q *progressQueue) take(now time.Time) application.ProgressBatch {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(len(q.items), application.MaxProgressItems)
	batch := application.ProgressBatch{Items: append([]application.ProgressItem(nil), q.items[:n]...), Dropped: q.dropped}
	q.items = append(q.items[:0:0], q.items[n:]...)
	q.dropped = 0
	q.text = 0
	for _, item := range q.items {
		if item.Kind.Streamed() {
			q.text += utf8.RuneCountInString(item.Text)
		}
	}
	q.textAt = now
	return batch
}

// empty reports nothing left to send.
func (q *progressQueue) empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed || (len(q.items) == 0 && q.dropped == 0)
}

// close drops what waits and refuses what comes.
func (q *progressQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed, q.items, q.dropped, q.text = true, nil, 0, 0
}
