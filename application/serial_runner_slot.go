package application

import (
	"context"
	"sync"
)

// unlockedRuntime lets go of the runner's state lock while the runtime
// runs: a run lasts up to the bot's task timeout (axlr-tui, 15 minutes
// by default) and the node calls that share the lock (NextAssignment,
// Report, Delegate, Emit, Subscribe) must not wait for it. The runner's
// slot is still held, so the host carries one in-process run at a time.
//
// Invariant: Execute is only ever called with mu held by the calling
// goroutine (the runner's Run, RunNext, Decide and ExpireApprovals take
// it, and a settle that decides a call by policy runs under it again).
// It takes mu back before it returns, so what follows the run (the
// settle) runs under the lock as before.
type unlockedRuntime struct {
	inner RuntimeExecutor
	mu    *sync.Mutex
}

func (u unlockedRuntime) Execute(ctx context.Context, request ExecutionRequest) (ExecutionReport, error) {
	u.mu.Unlock()
	defer u.mu.Lock()
	return u.inner.Execute(ctx, request)
}

// unlockedDecisions is unlockedRuntime for the decisions: it lets go of
// the state lock while the runtime carries a decision, under the same
// invariant (Decide is only ever called with mu held by the caller).
type unlockedDecisions struct {
	inner DecisionExecutor
	mu    *sync.Mutex
}

func (u unlockedDecisions) Decide(ctx context.Context, request DecisionRequest) (ExecutionReport, error) {
	u.mu.Unlock()
	defer u.mu.Lock()
	return u.inner.Decide(ctx, request)
}
