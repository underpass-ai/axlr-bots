package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetOverview counts nodes, bots and tasks by status, the tasks created
// since a moment by status, and the executions started since then with
// the cost they reported.
type GetOverview struct {
	deps Dependencies
}

// NewGetOverview wires the use case.
func NewGetOverview(deps Dependencies) GetOverview { return GetOverview{deps: deps} }

// Execute answers the overview since the moment; a zero moment is the
// start of today on the control plane's clock.
func (uc GetOverview) Execute(ctx context.Context, since time.Time) (Overview, error) {
	if since.IsZero() {
		now := uc.deps.Clock.Now()
		year, month, day := now.Date()
		since = time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	}
	overview := Overview{
		Since:      since,
		Nodes:      zeroCounts(string(domain.NodeRegistered), string(domain.NodeOnline), string(domain.NodeOffline), string(domain.NodeRevoked)),
		Bots:       zeroCounts(string(domain.BotDraft), string(domain.BotRegistered), string(domain.BotEnabled), string(domain.BotPaused)),
		Tasks:      zeroCounts(taskStatuses()...),
		TasksSince: zeroCounts(taskStatuses()...),
	}
	nodes, err := uc.deps.Nodes.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, node := range nodes {
		overview.Nodes[node.Status().String()]++
	}
	bots, err := uc.deps.Bots.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, bot := range bots {
		overview.Bots[bot.Status().String()]++
	}
	tasks, err := uc.deps.Tasks.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, task := range tasks {
		overview.Tasks[task.Status().String()]++
		if !task.CreatedAt().Before(since) {
			overview.TasksSince[task.Status().String()]++
		}
	}
	executions, err := uc.deps.Executions.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, execution := range executions {
		if execution.StartedAt().Before(since) {
			continue
		}
		overview.ExecutionsSince++
		overview.CostUSDSince += execution.Usage().CostUSD
	}
	return overview, nil
}

// taskStatuses names every task status, in the order a task goes
// through them.
func taskStatuses() []string {
	return []string{
		string(domain.TaskPending), string(domain.TaskRunning), string(domain.TaskWaitingApproval),
		string(domain.TaskCompleted), string(domain.TaskFailed), string(domain.TaskExpired), string(domain.TaskCancelled),
	}
}

// zeroCounts is a count of zero for each name.
func zeroCounts(names ...string) map[string]int {
	counts := make(map[string]int, len(names))
	for _, name := range names {
		counts[name] = 0
	}
	return counts
}
