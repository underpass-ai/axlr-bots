package domain

import (
	"encoding/json"
	"time"
)

// DeliverySnapshot is a delivery as plain data.
type DeliverySnapshot struct {
	ID            string          `json:"id"`
	BotID         string          `json:"botId"`
	TaskID        string          `json:"taskId"`
	ExecutionID   string          `json:"executionId"`
	CallID        string          `json:"callId,omitempty"`
	Target        string          `json:"target"`
	Kind          string          `json:"kind"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"nextAttemptAt"`
	LastError     string          `json:"lastError,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}
