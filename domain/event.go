package domain

import "time"

// Event is a fact the control plane publishes about a bot or a task
// (vision §11.2), with the schema version a real implementation needs.
type Event struct {
	ID            EventID        `json:"eventId"`
	SchemaVersion SchemaVersion  `json:"schemaVersion"`
	Type          EventType      `json:"type"`
	TenantID      TenantID       `json:"tenantId"`
	BotID         BotID          `json:"botId"`
	TaskID        TaskID         `json:"taskId,omitempty"`
	CorrelationID CorrelationID  `json:"correlationId,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	Payload       map[string]any `json:"payload,omitempty"`
}

// NewEvent builds an event under the current schema and validates it.
func NewEvent(id EventID, kind EventType, tenant TenantID, bot BotID, task TaskID, correlation CorrelationID, createdAt time.Time, payload map[string]any) (Event, error) {
	event := Event{
		ID: id, SchemaVersion: EventSchemaV1, Type: kind, TenantID: tenant, BotID: bot,
		TaskID: task, CorrelationID: correlation, CreatedAt: createdAt, Payload: payload,
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

// Validate checks the fields every event must carry.
func (e Event) Validate() error {
	switch {
	case e.ID == "":
		return invalid("event id", "must not be empty")
	case e.SchemaVersion == "":
		return invalid("schema version", "must not be empty")
	case e.TenantID == "":
		return invalid("tenant id", "must not be empty")
	case e.BotID == "":
		return invalid("bot id", "must not be empty")
	case e.CreatedAt.IsZero():
		return invalid("created at", "must not be zero")
	}
	_, err := ParseEventType(e.Type.String())
	return err
}
