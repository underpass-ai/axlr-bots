package unixapi

import (
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAnIncidentResponseCarriesTheWaitOfItsCurrentTask(t *testing.T) {
	at := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	incident, err := domain.NewIncident("inc-1", "ops", "triage", "g", "manual", "Drift in shop", domain.Sev3, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.AttachTask("task-1", at); err != nil {
		t.Fatal(err)
	}
	task, err := domain.RestoreTask(domain.TaskSnapshot{
		ID: "task-1", BotID: "triage", Objective: "Find the cause", Status: "WaitingApproval", IdempotencyKey: "k", CorrelationID: "corr-1",
		ExecutionIDs: []string{"exec-1"}, CreatedAt: at, UpdatedAt: at, IncidentID: "inc-1",
		Approval: &domain.ApprovalWait{CallID: "c1", Tool: "local_write", Since: at, Deadline: at.Add(time.Minute), OnExpire: domain.ExpireDeny},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := newIncidentResponse(application.IncidentReport{Incident: incident, NeedsPerson: true, Current: &task, Approvals: application.IncidentApprovals{Waiting: 1}, Whole: true})
	if !response.NeedsYou || response.CurrentTask == nil || response.CurrentTask.Phase != "triage" || response.CurrentTask.Status != "WaitingApproval" ||
		response.CurrentTask.Approval == nil || response.CurrentTask.Approval.Tool != "local_write" || response.Approvals.Waiting != 1 || response.ID != "inc-1" {
		t.Errorf("response = %+v %+v", response, response.CurrentTask)
	}
	gone := newIncidentResponse(application.IncidentReport{Incident: incident, Whole: true})
	if gone.CurrentTask != nil || gone.Approvals == nil {
		t.Errorf("a get whose current task is gone: %+v", gone)
	}
}
