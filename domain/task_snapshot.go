package domain

import (
	"encoding/json"
	"time"
)

// TaskSnapshot is a task as plain data. CorrelationID is the task's
// trace id; Trigger and Input say how it was created (a manual task
// has no input). Approval is the last wait for a person's decision:
// its deadline while the task waits, its outcome once it ended.
// Delegation is where a delegated task came from; Children are the
// tasks this one delegated, and the granted counts what it handed them.
type TaskSnapshot struct {
	ID             string             `json:"id"`
	BotID          string             `json:"botId"`
	Objective      string             `json:"objective"`
	Status         string             `json:"status"`
	IdempotencyKey string             `json:"idempotencyKey"`
	CorrelationID  string             `json:"correlationId,omitempty"`
	Trigger        TriggerRefSnapshot `json:"trigger"`
	Input          json.RawMessage    `json:"input,omitempty"`
	ExecutionIDs   []string           `json:"executionIds"`
	FailureReason  string             `json:"failureReason,omitempty"`
	Approval       *ApprovalWait      `json:"approval,omitempty"`
	Delegation     *TaskLineage       `json:"delegation,omitempty"`
	Children       []string           `json:"children,omitempty"`
	GrantedModel   int                `json:"grantedModelCalls,omitempty"`
	GrantedTools   int                `json:"grantedToolCalls,omitempty"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

// TriggerRefSnapshot is a trigger reference as plain data; empty means
// manual (a task written before triggers had names).
type TriggerRefSnapshot struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}
