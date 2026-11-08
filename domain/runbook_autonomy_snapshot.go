package domain

import "time"

// RunbookAutonomySnapshot is a runbook's standing as plain data. Step is
// where it stands, Start the step it began at; Totals counts every
// signal since it began, Earned those since its last lowering, the ones
// a promotion reads. History lists every signal, oldest first.
type RunbookAutonomySnapshot struct {
	ID        string           `json:"id"`
	TenantID  string           `json:"tenantId"`
	BotID     string           `json:"botId"`
	Runbook   string           `json:"runbook"`
	Namespace string           `json:"namespace"`
	Step      string           `json:"step"`
	Start     string           `json:"start"`
	Totals    AutonomyCounters `json:"totals"`
	Earned    AutonomyCounters `json:"earned"`
	History   []AutonomyEntry  `json:"history,omitempty"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}
