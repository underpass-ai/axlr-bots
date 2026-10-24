package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// CancelResponse is the task after a cancel, the execution it stopped
// (when one was running) and whether the call changed anything.
type CancelResponse struct {
	Task      domain.TaskSnapshot       `json:"task"`
	Execution *domain.ExecutionSnapshot `json:"execution,omitempty"`
	Changed   bool                      `json:"changed"`
}
