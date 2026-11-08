package application

import "time"

// PostmortemDecision is one decision on one of an incident's gated
// calls: which task and tool, what was decided (approved, denied,
// expired or a first approval of two), by whom and when. A policy that
// decided is named as one (policy:autoApprove).
type PostmortemDecision struct {
	At      time.Time `json:"at"`
	TaskID  string    `json:"taskId"`
	Phase   string    `json:"phase,omitempty"`
	Cycle   int       `json:"cycle,omitempty"`
	Tool    string    `json:"tool,omitempty"`
	Outcome string    `json:"outcome"`
	By      string    `json:"by,omitempty"`
}
