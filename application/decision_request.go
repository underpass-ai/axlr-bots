package application

import (
	"errors"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Decisions a person can take on a pending tool call.
const (
	DecisionApprove = "approve"
	DecisionDeny    = "deny"
)

// DecisionRequest decides the pending call of a runtime session: approve
// or deny, optionally checking the call id, under a new execution and
// the same policy the first execution ran under.
type DecisionRequest struct {
	ExecutionID   domain.ExecutionID
	TaskID        domain.TaskID
	BotID         domain.BotID
	CorrelationID domain.CorrelationID
	SessionID     string
	CallID        string
	Decision      string
	Level         domain.AutonomyLevel
	AllowedTools  []string
	ToolRules     domain.ToolRules
	Timeout       time.Duration
	// Delegation, Events and DelegationSocket are as on an
	// ExecutionRequest: the session goes on with the tools it had.
	Delegation       *DelegationOffer
	Events           *EventsOffer
	DelegationSocket string
	// OnSaid is as on an ExecutionRequest.
	OnSaid func(SaidReply) `json:"-"`
}

// Validate checks the request names a session and a known decision.
func (r DecisionRequest) Validate() error {
	switch {
	case r.SessionID == "":
		return errors.New("a decision needs the session id")
	case !IsDecision(r.Decision):
		return errors.New("a decision is approve or deny")
	}
	return nil
}

// IsDecision reports whether the word is a decision a person can take.
func IsDecision(word string) bool {
	return word == DecisionApprove || word == DecisionDeny
}

// NodeTools names the tools the node serves the decision's execution.
func (r DecisionRequest) NodeTools() []string { return nodeTools(r.Delegation, r.Events) }
