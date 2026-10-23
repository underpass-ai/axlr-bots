package axlr

import (
	"errors"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// DecisionRequest decides the pending call of a runtime session: approve
// or deny, optionally checking the call id, under a new execution.
type DecisionRequest struct {
	ExecutionID domain.ExecutionID
	BotID       domain.BotID
	SessionID   string
	CallID      string
	Decision    string
	Timeout     time.Duration
}

// Validate checks the request names a session and a known decision.
func (r DecisionRequest) Validate() error {
	switch {
	case r.SessionID == "":
		return errors.New("axlr: a decision needs the session id")
	case r.Decision != "approve" && r.Decision != "deny":
		return errors.New("axlr: a decision is approve or deny")
	}
	return nil
}
