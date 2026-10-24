package domain

import "time"

// ExecutionSnapshot is an execution as plain data.
type ExecutionSnapshot struct {
	ID            string        `json:"id"`
	TaskID        string        `json:"taskId"`
	BotID         string        `json:"botId"`
	StartedAt     time.Time     `json:"startedAt"`
	EndedAt       *time.Time    `json:"endedAt,omitempty"`
	Outcome       string        `json:"outcome"`
	Result        Result        `json:"result"`
	Usage         Usage         `json:"usage"`
	FailureReason string        `json:"failureReason,omitempty"`
	TracePath     string        `json:"tracePath,omitempty"`
	Policy        *PolicyRecord `json:"policy,omitempty"`
	Kind          string        `json:"kind,omitempty"`
	NodeID        string        `json:"nodeId,omitempty"`
	LeaseUntil    *time.Time    `json:"leaseUntil,omitempty"`
}
