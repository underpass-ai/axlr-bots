package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// LeaseDuration is how long a node holds an execution before it must
// renew; an execution whose lease expires is failed by reconciliation.
const LeaseDuration = 2 * time.Minute

// Assignment is one execution handed to a node: a run of the task's
// objective or a decision on a pending call, with the lease the node
// must renew while it works.
type Assignment struct {
	ExecutionID domain.ExecutionID
	Kind        domain.ExecutionKind
	LeaseUntil  time.Time
	Run         *ExecutionRequest
	Decide      *DecisionRequest
}
