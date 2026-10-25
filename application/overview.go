package application

import "time"

// Overview is the operator's dashboard: what the control plane holds now,
// counted by status, and what happened since a moment (the start of the
// operator's day). Each map is keyed by the status name and lists every
// status, at zero when nothing is in it.
type Overview struct {
	Since           time.Time      `json:"since"`
	Nodes           map[string]int `json:"nodes"`
	Bots            map[string]int `json:"bots"`
	Tasks           map[string]int `json:"tasks"`
	TasksSince      map[string]int `json:"tasksSince"`
	ExecutionsSince int            `json:"executionsSince"`
	CostUSDSince    float64        `json:"costUsdSince"`
}
