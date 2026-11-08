package domain

// IncidentTaskSnapshot is a task of an incident as plain data: the task,
// the bot that ran it, the phase it worked on (triage, fix, apply, verify or postmortem) and the
// incident's cycle it belongs to.
type IncidentTaskSnapshot struct {
	TaskID string `json:"taskId"`
	BotID  string `json:"botId"`
	Phase  string `json:"phase"`
	Cycle  int    `json:"cycle"`
}
