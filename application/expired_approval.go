package application

import "github.com/underpass-ai/axlr-bots/domain"

// ExpiredApproval is what the expiry of one task did: the action (deny
// or cancel; empty when the task needed none), the task after it and,
// for a denial, the execution that carries it.
type ExpiredApproval struct {
	TaskID    domain.TaskID
	Action    domain.ApprovalExpiry
	Task      domain.Task
	Execution *domain.Execution
}
