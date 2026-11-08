package domain

import "time"

// IncidentPostmortemSnapshot is an incident's postmortem as plain data.
// Cycle is the incident's cycle it was written for; TaskID and BotID the
// last task launched to write it and the bot that took it; AuthorTaskID
// the task whose answer Markdown is, which is another while a sent-back
// postmortem is being rewritten. Note, ReviewedBy and ReviewedAt are the
// last send-back; Reason why the last task did not write it. SendBacks
// counts the send-backs of this cycle's postmortem. ApprovedBy and
// ApprovedAt say who approved it and when.
type IncidentPostmortemSnapshot struct {
	State        string     `json:"state"`
	Cycle        int        `json:"cycle"`
	TaskID       string     `json:"taskId"`
	BotID        string     `json:"botId"`
	AuthorTaskID string     `json:"authorTaskId,omitempty"`
	Markdown     string     `json:"markdown,omitempty"`
	Note         string     `json:"note,omitempty"`
	ReviewedBy   string     `json:"reviewedBy,omitempty"`
	ReviewedAt   *time.Time `json:"reviewedAt,omitempty"`
	Reason       string     `json:"reason,omitempty"`
	SendBacks    int        `json:"sendBacks,omitempty"`
	ApprovedBy   string     `json:"approvedBy,omitempty"`
	ApprovedAt   *time.Time `json:"approvedAt,omitempty"`
}
