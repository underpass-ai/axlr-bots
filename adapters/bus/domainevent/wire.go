package domainevent

import (
	"encoding/json"
	"fmt"

	"github.com/underpass-ai/axlr-bots/application"
)

// Message is a crossing event as a bus adapter sends it: the subject, the
// id a broker drops a repeat by (MsgIDHeader) and the payload, the DTO as
// JSON.
type Message struct {
	Subject string
	ID      string
	Payload []byte
}

// Encode writes the crossing event for the wire.
func Encode(event application.CrossingEvent) (Message, error) {
	dto := ToDTO(event.Event, event.Tenant, event.TraceID, event.Crossings, event.Audience)
	payload, err := json.Marshal(dto)
	if err != nil {
		return Message{}, err
	}
	return Message{Subject: Subject(event.Tenant, event.Event.Team(), event.Event.Name()), ID: dto.ID, Payload: payload}, nil
}

// Decode reads a crossing event off the wire; an error is a message that
// is no domain event of the spec (a poison message: retrying it mends
// nothing).
func Decode(payload []byte) (application.CrossingEvent, error) {
	var dto DTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		return application.CrossingEvent{}, fmt.Errorf("domain event: %w", err)
	}
	e, tenant, origin, audience, err := FromDTO(dto)
	if err != nil {
		return application.CrossingEvent{}, err
	}
	return application.CrossingEvent{Tenant: tenant, Event: e, Audience: audience, TraceID: origin.TraceID, Crossings: origin.Crossings}, nil
}
