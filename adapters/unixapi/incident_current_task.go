package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// IncidentCurrentTask is the task an incident's next move waits on: its
// bot, phase and status, and the approval it waits for or last waited
// for. A panel decides it through the task's decide route.
type IncidentCurrentTask struct {
	TaskID   string               `json:"taskId"`
	BotID    string               `json:"botId"`
	Phase    string               `json:"phase"`
	Status   string               `json:"status"`
	Approval *domain.ApprovalWait `json:"approval,omitempty"`
}
