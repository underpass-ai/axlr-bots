package nodeagent

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// progressSender hands one execution's progress to the control plane
// while it runs: the run pushes items into its queue, and one goroutine
// sends them in batches, so a slow or absent control plane never holds
// the run up. A batch that does not go is logged and lost; the report
// at the end still says everything.
type progressSender struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	queue     *progressQueue
	every     time.Duration
	chars     int
	timeout   time.Duration
	log       application.LogPrinter

	stop chan struct{}
	done chan struct{}
	// failed counts the batches that did not go; lost the items dropped
	// on the way.
	failed int
	lost   int
}

// startProgress starts the sender of an execution's progress.
func (a Agent) startProgress(ctx context.Context, id domain.NodeID, execution domain.ExecutionID) *progressSender {
	s := &progressSender{
		transport: a.Transport, node: id, execution: execution, queue: newProgressQueue(a.Options.progressQueue()),
		every: a.Options.progressEvery(), chars: a.Options.progressChars(), timeout: a.Options.progressTimeout(), log: a.execLog(id, execution),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go s.run(ctx)
	return s
}

// push queues what the run did; it never blocks.
func (s *progressSender) push(item application.ProgressItem) {
	now := time.Now()
	if item.At.IsZero() {
		item.At = now
	}
	s.queue.push(item, now)
}

// finish sends what is left and waits for the sender, before the report
// ends the execution and the control plane takes no more.
func (s *progressSender) finish() {
	close(s.stop)
	<-s.done
	if s.failed > 0 || s.lost > 0 {
		s.log.Printf("node: execution %s: %d progress batches not handed over, %d items dropped", s.execution, s.failed, s.lost)
	}
}

func (s *progressSender) run(ctx context.Context) {
	defer close(s.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		send, after := s.queue.due(time.Now(), s.every, s.chars)
		if send {
			s.send()
			continue
		}
		var due <-chan time.Time
		if after > 0 {
			timer.Reset(after)
			due = timer.C
		}
		select {
		case <-s.queue.wake:
		case <-due:
		case <-s.stop:
			for !s.queue.empty() {
				s.send()
			}
			return
		case <-ctx.Done():
			return
		}
		timer.Stop()
	}
}

// send hands one batch over. A control plane older than the route, or
// one that keeps no more for the execution, closes the queue.
func (s *progressSender) send() {
	batch := s.queue.take(time.Now())
	if batch.Dropped > 0 {
		s.lost += batch.Dropped
		s.log.Printf("node: execution %s: %d progress items dropped: the control plane was slower than the run", s.execution, batch.Dropped)
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	answer, err := s.transport.Progress(ctx, s.node, s.execution, batch)
	switch {
	case domain.IsNotFound(err):
		s.log.Printf("node: execution %s: the control plane takes no progress (%v); the run's report says it all", s.execution, err)
		s.queue.close()
	case err != nil:
		if s.failed == 0 {
			s.log.Printf("node: execution %s: progress not handed over: %v", s.execution, err)
		}
		s.failed++
	case answer.Full:
		s.log.Printf("node: execution %s: the control plane keeps no more of its progress", s.execution)
		s.queue.close()
	}
}
