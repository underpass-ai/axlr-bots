package application

import "time"

// Overview is the operator's dashboard: what the control plane holds now,
// counted by status, and what happened since a moment (the start of the
// operator's day). Each map is keyed by the status name and lists every
// status, at zero when nothing is in it. CostUnknownSince counts the
// executions since whose cost is not known (a run stopped before it said):
// CostUSDSince is then at least what it says.
type Overview struct {
	Since            time.Time      `json:"since"`
	Nodes            map[string]int `json:"nodes"`
	Bots             map[string]int `json:"bots"`
	Tasks            map[string]int `json:"tasks"`
	TasksSince       map[string]int `json:"tasksSince"`
	ExecutionsSince  int            `json:"executionsSince"`
	CostUSDSince     float64        `json:"costUsdSince"`
	CostUnknownSince int            `json:"costUnknownSince,omitempty"`
}
