package application

import "github.com/underpass-ai/axlr-bots/domain"

// IncidentApprovals counts the approvals an incident's tasks waited for,
// by how each ended: what the product's rule of one approval per
// incident is read against (decided 8 November 2026; the control plane
// does not enforce it). A task keeps its last wait only, so a task that
// waited twice counts once, by its last.
type IncidentApprovals struct {
	Approved int
	Denied   int
	Expired  int
	// Waiting is a wait no decision, expiry or cancel has ended yet.
	Waiting int
}

// add counts the task's wait, if it had one.
func (a *IncidentApprovals) add(task domain.Task) {
	wait, ok := task.Approval()
	if !ok {
		return
	}
	switch wait.Outcome {
	case "":
		a.Waiting++
	case domain.ApprovalApproved:
		a.Approved++
	case domain.ApprovalDenied:
		a.Denied++
	case domain.ApprovalExpired:
		a.Expired++
	}
}
