package application

import (
	"context"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// RenewLease extends a node's lease on an execution it is running.
type RenewLease struct {
	deps Dependencies
}

// NewRenewLease wires the use case.
func NewRenewLease(deps Dependencies) RenewLease { return RenewLease{deps: deps} }

// Execute renews the lease and returns its new expiry. An execution the
// node does not hold, or one that has ended, is refused.
func (uc RenewLease) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID) (time.Time, error) {
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if execution.NodeID() != node {
		return time.Time{}, domain.StateError{Subject: "execution " + id.String(), Current: "assigned to " + orNobody(execution.NodeID()), Attempted: "renew a lease for node " + node.String()}
	}
	until := uc.deps.Clock.Now().Add(LeaseDuration)
	if err := execution.Lease(until); err != nil {
		return time.Time{}, err
	}
	return until, uc.deps.Executions.Save(ctx, execution)
}

func orNobody(node domain.NodeID) string {
	if node == "" {
		return "nobody"
	}
	return node.String()
}
