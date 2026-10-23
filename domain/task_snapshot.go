package domain

import "time"

// TaskSnapshot is a task as plain data.
type TaskSnapshot struct {
	ID             string    `json:"id"`
	BotID          string    `json:"botId"`
	Objective      string    `json:"objective"`
	Status         string    `json:"status"`
	IdempotencyKey string    `json:"idempotencyKey"`
	CorrelationID  string    `json:"correlationId,omitempty"`
	ExecutionIDs   []string  `json:"executionIds"`
	FailureReason  string    `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
