package domain

import "time"

// IncidentSnapshot is an incident as plain data. GroupKey is what the
// source groups alerts by (Alertmanager's groupKey); Fingerprints are the
// alerts seen in it. Cycle counts the opens: 1 for the first, one more
// for each reopening; TaskIDs lists the triage tasks, one per cycle,
// oldest first. Diagnosis is the answer of the last completed triage
// task; Reason says why it was escalated or resolved. Repeats counts the
// alerts received while it was already open; CostUSD sums what its tasks'
// executions reported.
type IncidentSnapshot struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenantId"`
	BotID        string     `json:"botId"`
	GroupKey     string     `json:"groupKey"`
	Source       string     `json:"source"`
	Title        string     `json:"title"`
	Severity     string     `json:"severity"`
	Status       string     `json:"status"`
	Fingerprints []string   `json:"fingerprints,omitempty"`
	Cycle        int        `json:"cycle"`
	TaskIDs      []string   `json:"taskIds,omitempty"`
	Diagnosis    string     `json:"diagnosis,omitempty"`
	Reason       string     `json:"reason,omitempty"`
	Repeats      int        `json:"repeats,omitempty"`
	CostUSD      float64    `json:"costUsd,omitempty"`
	OpenedAt     time.Time  `json:"openedAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	LastSeenAt   time.Time  `json:"lastSeenAt"`
	ResolvedAt   *time.Time `json:"resolvedAt,omitempty"`
}
