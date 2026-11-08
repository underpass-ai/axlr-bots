package domain

import "time"

// IncidentSnapshot is an incident as plain data. Namespace is the first
// namespace label its alerts carried (absent when they carried none, and
// on an incident stored before it was kept); Namespaces lists them all
// when there are two or more. GroupKey is what the
// source groups alerts by (Alertmanager's groupKey); Fingerprints are the
// alerts seen in it, the first 64, and ResolvedFingerprints those of them
// the source said went green. Cycle counts the opens: 1 for the first, one more
// for each reopening; Tasks lists the tasks that worked on it, oldest
// first, each with its bot, its phase (triage, fix, apply, verify or postmortem) and its cycle, one
// per phase in each cycle (one more postmortem task per send-back), and TaskIDs their ids in the same order (an
// incident stored before tasks had phases has only TaskIDs: its triage
// tasks, one per cycle). Diagnosis is the answer of the last completed
// triage task; Proposal the answer of the fix task of that diagnosis;
// Reason says why it was escalated, resolved or sent back to Open. Repeats counts the
// alerts received while it was already open; Retries the triages an alert
// started again on it while it was Escalated, since its last proposal or reopening; SendBacks the times it went back
// to Open from a proposal (a denial, an expiry, a failed apply, a red
// verify) since it was last reopened (IncidentMaxSendBacks escalates it); CostUSD sums what its tasks'
// executions reported, and CostUnknown says one of them stopped before
// it said what it cost (CostUSD is then a floor); CostedExecutions the
// executions of its own tasks whose cost it added, each once.
type IncidentSnapshot struct {
	ID                   string                 `json:"id"`
	TenantID             string                 `json:"tenantId"`
	BotID                string                 `json:"botId"`
	GroupKey             string                 `json:"groupKey"`
	Source               string                 `json:"source"`
	Title                string                 `json:"title"`
	Severity             string                 `json:"severity"`
	Namespace            string                 `json:"namespace,omitempty"`
	Namespaces           []string               `json:"namespaces,omitempty"`
	Status               string                 `json:"status"`
	Fingerprints         []string               `json:"fingerprints,omitempty"`
	ResolvedFingerprints []string               `json:"resolvedFingerprints,omitempty"`
	Cycle                int                    `json:"cycle"`
	TaskIDs              []string               `json:"taskIds,omitempty"`
	Tasks                []IncidentTaskSnapshot `json:"tasks,omitempty"`
	Diagnosis            string                 `json:"diagnosis,omitempty"`
	Proposal             string                 `json:"proposal,omitempty"`
	Reason               string                 `json:"reason,omitempty"`
	Repeats              int                    `json:"repeats,omitempty"`
	Retries              int                    `json:"retries,omitempty"`
	SendBacks            int                    `json:"sendBacks,omitempty"`
	CostUSD              float64                `json:"costUsd,omitempty"`
	CostUnknown          bool                   `json:"costUnknown,omitempty"`
	CostedExecutions     []string               `json:"costedExecutions,omitempty"`
	OpenedAt             time.Time              `json:"openedAt"`
	UpdatedAt            time.Time              `json:"updatedAt"`
	LastSeenAt           time.Time              `json:"lastSeenAt"`
	ResolvedAt           *time.Time             `json:"resolvedAt,omitempty"`
	// Postmortem is what a resolved incident's postmortem task wrote and
	// where it stands; absent before one started.
	Postmortem *IncidentPostmortemSnapshot `json:"postmortem,omitempty"`
}
