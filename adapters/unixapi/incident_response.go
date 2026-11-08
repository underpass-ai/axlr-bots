package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// IncidentResponse is an incident as the incident routes list and get
// it: its snapshot, whether it needs a person now (its current task
// waits for an approval where the incident stands) and, for one incident
// read on its own, its current task and the approvals its tasks waited
// for.
type IncidentResponse struct {
	domain.IncidentSnapshot
	NeedsYou    bool                       `json:"needsYou,omitempty"`
	CurrentTask *IncidentCurrentTask       `json:"currentTask,omitempty"`
	Approvals   *IncidentApprovalsResponse `json:"approvals,omitempty"`
}

// newIncidentResponse maps the application's report.
func newIncidentResponse(report application.IncidentReport) IncidentResponse {
	response := IncidentResponse{IncidentSnapshot: report.Incident.Snapshot(), NeedsYou: report.NeedsPerson}
	if task := report.Current; task != nil {
		current := IncidentCurrentTask{TaskID: task.ID().String(), BotID: task.BotID().String(), Status: task.Status().String()}
		if attached, ok := report.Incident.CurrentTask(); ok {
			current.Phase = attached.Phase().String()
		}
		if wait, ok := task.Approval(); ok {
			current.Approval = &wait
		}
		response.CurrentTask = &current
	}
	if report.Whole {
		a := report.Approvals
		response.Approvals = &IncidentApprovalsResponse{Approved: a.Approved, Denied: a.Denied, Expired: a.Expired, Waiting: a.Waiting}
	}
	return response
}
