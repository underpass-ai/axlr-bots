package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DelegationAnswer is what the control plane tells the node about a
// delegation: the refusal, or the child task with its depth, its grant
// and its state (its answer once it Completed, the reason once it
// Failed). WaitUntil is how long the parent may wait for it: its own
// deadline less a margin.
type DelegationAnswer struct {
	Refused       *domain.DelegationRefusal
	TaskID        domain.TaskID
	BotID         domain.BotID
	Status        domain.TaskStatus
	Created       bool
	Depth         int
	Grant         domain.DelegationGrant
	Done          bool
	Answer        string
	FailureReason string
	WaitUntil     time.Time
}
