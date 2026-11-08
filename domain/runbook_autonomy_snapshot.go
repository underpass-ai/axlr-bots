package domain

import "time"

// RunbookAutonomySnapshot is a runbook's standing as plain data. Step is
// where it stands, Start the step it began at; Totals counts every
// signal since it began, Earned those since its last lowering, the ones
// a promotion reads. Tool is the write the record was earned with.
// History lists the signals, oldest first; Outstanding the applies whose
// end is not recorded, which hold the runbook to the card.
type RunbookAutonomySnapshot struct {
	ID          string                `json:"id"`
	TenantID    string                `json:"tenantId"`
	BotID       string                `json:"botId"`
	Runbook     string                `json:"runbook"`
	Namespace   string                `json:"namespace"`
	Tool        string                `json:"tool,omitempty"`
	Step        string                `json:"step"`
	Start       string                `json:"start"`
	Totals      AutonomyCounters      `json:"totals"`
	Earned      AutonomyCounters      `json:"earned"`
	History     []AutonomyEntry       `json:"history,omitempty"`
	Outstanding []AutonomyOutstanding `json:"outstanding,omitempty"`
	CreatedAt   time.Time             `json:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
}
