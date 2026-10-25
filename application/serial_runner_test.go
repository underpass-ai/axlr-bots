package application

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// overlapRuntime fails the test when two executions overlap.
type overlapRuntime struct {
	active  atomic.Int32
	overlap atomic.Bool
}

func (o *overlapRuntime) Execute(context.Context, ExecutionRequest) (ExecutionReport, error) {
	if o.active.Add(1) > 1 {
		o.overlap.Store(true)
	}
	time.Sleep(5 * time.Millisecond)
	o.active.Add(-1)
	return ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}, nil
}

func TestSerialRunnerNeverOverlaps(t *testing.T) {
	h := newHarness()
	runtime := &overlapRuntime{}
	h.deps.Runtime = runtime
	bot := enabledBot(t, h)
	var ids []domain.TaskID
	for i := 0; i < 6; i++ {
		ids = append(ids, submit(t, h, bot, "k-"+string(rune('a'+i))).Task.ID())
	}
	runner := NewSerialRunner(h.deps)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id domain.TaskID) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				_, _ = runner.Run(context.Background(), id)
			case 1:
				_, _, _ = runner.RunNext(context.Background())
			default:
				_, _ = runner.Decide(context.Background(), id, DecisionApprove, "")
			}
		}(i, id)
	}
	wg.Wait()
	if runtime.overlap.Load() {
		t.Error("two executions overlapped")
	}
	for {
		_, ran, err := runner.RunNext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ran {
			break
		}
	}
	for _, id := range ids {
		task, _ := h.tasks.Get(context.Background(), id)
		if task.Status() != domain.TaskCompleted {
			t.Errorf("%s is %s", id, task.Status())
		}
	}
}
