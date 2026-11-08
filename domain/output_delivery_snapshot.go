package domain

import (
	"encoding/json"
	"time"
)

// OutputDeliverySnapshot is an output delivery as plain data.
type OutputDeliverySnapshot struct {
	ID            string          `json:"id"`
	Output        string          `json:"output"`
	Kind          string          `json:"kind"`
	IncidentID    string          `json:"incidentId"`
	TenantID      string          `json:"tenantId"`
	EventID       string          `json:"eventId"`
	Event         string          `json:"event"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"nextAttemptAt"`
	LastError     string          `json:"lastError,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}
