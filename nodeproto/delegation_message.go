package nodeproto

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DelegationMessage is the control plane's answer about a delegation:
// the refusal, or the child task, its depth, its grant and its state
// (its answer once Completed). WaitUntil is when the parent must stop
// waiting for it.
type DelegationMessage struct {
	Refused       *domain.DelegationRefusal `json:"refused,omitempty"`
	TaskID        string                    `json:"taskId,omitempty"`
	BotID         string                    `json:"botId,omitempty"`
	Status        string                    `json:"status,omitempty"`
	Created       bool                      `json:"created,omitempty"`
	Depth         int                       `json:"depth,omitempty"`
	Grant         *domain.DelegationGrant   `json:"grant,omitempty"`
	Done          bool                      `json:"done,omitempty"`
	Answer        string                    `json:"answer,omitempty"`
	FailureReason string                    `json:"failureReason,omitempty"`
	WaitUntil     time.Time                 `json:"waitUntil"`
}
