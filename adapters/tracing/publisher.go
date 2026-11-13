package tracing

import (
	"context"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// buildTimeout bounds one trace build: a few reads and a handful of
// spans handed to a batching exporter.
const buildTimeout = 30 * time.Second

// rememberedTasks is how many ended tasks the publisher remembers it
// traced, so the two events a task can end with (a cancel and its
// preemption) make one trace.
const rememberedTasks = 4096

// Publisher decorates an application.EventPublisher: the event goes to
// the publisher it wraps first, and when it ends a task the task's trace
// is built in the background. A trace that cannot be built is logged and
// never fails the event.
type Publisher struct {
	inner application.EventPublisher
	build TaskTrace
	log   func(format string, args ...any)

	waiting sync.WaitGroup
	mu      sync.Mutex
	traced  map[domain.TaskID]struct{}
	order   []domain.TaskID
}

// NewPublisher wraps inner. The log function may be nil.
func NewPublisher(inner application.EventPublisher, build TaskTrace, log func(format string, args ...any)) *Publisher {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Publisher{inner: inner, build: build, log: log, traced: map[domain.TaskID]struct{}{}}
}

// Publish hands the event on, then traces the task it ends.
func (p *Publisher) Publish(ctx context.Context, event domain.Event) error {
	if err := p.inner.Publish(ctx, event); err != nil {
		return err
	}
	if endsTask(event.Type) && event.TaskID != "" && p.remember(event.TaskID) {
		p.waiting.Add(1)
		go p.trace(context.WithoutCancel(ctx), event.TaskID)
	}
	return nil
}

// Wait blocks until the traces under way are built; the daemon calls it
// on the way out, before the provider is flushed.
func (p *Publisher) Wait() { p.waiting.Wait() }

func (p *Publisher) trace(ctx context.Context, task domain.TaskID) {
	defer p.waiting.Done()
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	built, err := p.build.Build(ctx, task)
	if err != nil {
		p.log("tracing: task %s: %v", task, err)
	}
	if err != nil || !built {
		p.forget(task)
	}
}

// remember says whether the task is new to the publisher.
func (p *Publisher) remember(task domain.TaskID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, seen := p.traced[task]; seen {
		return false
	}
	p.traced[task] = struct{}{}
	p.order = append(p.order, task)
	if len(p.order) > rememberedTasks {
		delete(p.traced, p.order[0])
		p.order = p.order[1:]
	}
	return true
}

func (p *Publisher) forget(task domain.TaskID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.traced, task)
}

// endsTask reports an event after which the task changes no further.
func endsTask(kind domain.EventType) bool {
	switch kind {
	case domain.EventTaskCompleted, domain.EventTaskFailed, domain.EventTaskCancelled, domain.EventTaskPreempted, domain.EventTaskExpired:
		return true
	}
	return false
}
