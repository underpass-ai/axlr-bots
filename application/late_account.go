package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// lateAccount keeps what a run consumed when its report comes after its
// execution ended: a cancel, a stop or an urgent task ends the execution
// first (its cost unknown), and the run, interrupted in process or
// stopped by its refused lease on a node, reports after. The report
// changes nothing else, but its usage and receipt are the best number
// there is: before, the run's cost counted as $0 (5 November 2026).
type lateAccount struct {
	deps Dependencies
	rec  recorder
}

func newLateAccount(deps Dependencies) lateAccount {
	return lateAccount{deps: deps, rec: recorder{deps: deps}}
}

// keep records the report's usage on the ended execution when the
// execution's cost is unknown and the report knows it.
func (l lateAccount) keep(ctx context.Context, task domain.Task, execution *domain.Execution, report ExecutionReport) error {
	changed, err := execution.AccountLate(report.Usage, report.Receipt)
	if err != nil || !changed {
		return err
	}
	if err := l.deps.Executions.Save(ctx, *execution); err != nil {
		return err
	}
	details := map[string]string{"task": task.ID().String(), "outcome": execution.Outcome().String(), "costUsd": strconv.FormatFloat(execution.Usage().CostUSD, 'f', -1, 64)}
	if err := l.rec.audit(ctx, "execution.account", execution.ID().String(), domain.AuditOK, details); err != nil {
		return err
	}
	return nil
}
