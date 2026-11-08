package domain

import (
	"bytes"
	"encoding/json"
	"time"
)

// OutputDelivery is one incident event announced to one output target:
// the body, fixed when it was queued so every attempt sends the same
// bytes, how many attempts were made and when the next is due. It lives
// in the approval notifications' outbox and follows their retries; it
// is its own type because it names an incident and an event, not a task
// waiting on a call.
type OutputDelivery struct {
	id            DeliveryID
	output        OutputName
	kind          OutputKind
	incident      IncidentID
	tenant        TenantID
	eventID       EventID
	event         EventType
	payload       json.RawMessage
	status        DeliveryStatus
	attempts      int
	nextAttemptAt time.Time
	lastError     string
	createdAt     time.Time
	updatedAt     time.Time
}

// NewOutputDelivery queues a delivery of the event to the target whose
// first attempt is due now.
func NewOutputDelivery(id DeliveryID, target OutputTarget, incident Incident, eventID EventID, event EventType, payload json.RawMessage, now time.Time) (OutputDelivery, error) {
	return newOutputDelivery(id, target.Name(), target.Kind(), incident.ID(), incident.TenantID(), eventID, event, payload, now)
}

func newOutputDelivery(id DeliveryID, output OutputName, kind OutputKind, incident IncidentID, tenant TenantID, eventID EventID, event EventType, payload json.RawMessage, now time.Time) (OutputDelivery, error) {
	if now.IsZero() {
		return OutputDelivery{}, invalid("created at", "must not be zero")
	}
	if incident == "" || tenant == "" || eventID == "" {
		return OutputDelivery{}, invalid("output delivery", "needs an incident, a tenant and an event")
	}
	if !event.AboutIncident() {
		return OutputDelivery{}, invalid("output delivery event", "must be an incident event")
	}
	// Compact, so the bytes sent are the same however the payload was
	// stored: a webhook's signature is over them.
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		return OutputDelivery{}, invalid("output delivery payload", "must be JSON")
	}
	return OutputDelivery{
		id: id, output: output, kind: kind, incident: incident, tenant: tenant, eventID: eventID, event: event,
		payload: json.RawMessage(compact.Bytes()), status: DeliveryPending, nextAttemptAt: now, createdAt: now, updatedAt: now,
	}, nil
}

// ID is the delivery's identifier, sent with every attempt.
func (d OutputDelivery) ID() DeliveryID { return d.id }

// Output is the name of the target it goes to.
func (d OutputDelivery) Output() OutputName { return d.output }

// Kind is the target's type when the delivery was queued.
func (d OutputDelivery) Kind() OutputKind { return d.kind }

// IncidentID is the incident it announces.
func (d OutputDelivery) IncidentID() IncidentID { return d.incident }

// TenantID is the incident's tenant.
func (d OutputDelivery) TenantID() TenantID { return d.tenant }

// EventID is the event it announces.
func (d OutputDelivery) EventID() EventID { return d.eventID }

// Event is the type of the event it announces.
func (d OutputDelivery) Event() EventType { return d.event }

// Payload is the body every attempt sends.
func (d OutputDelivery) Payload() json.RawMessage { return append(json.RawMessage(nil), d.payload...) }

// Status is where the delivery is.
func (d OutputDelivery) Status() DeliveryStatus { return d.status }

// Attempts counts the attempts made.
func (d OutputDelivery) Attempts() int { return d.attempts }

// NextAttemptAt is when the next attempt is due.
func (d OutputDelivery) NextAttemptAt() time.Time { return d.nextAttemptAt }

// LastError says why the last attempt failed, or why it was skipped.
func (d OutputDelivery) LastError() string { return d.lastError }

// CreatedAt is when the delivery was queued.
func (d OutputDelivery) CreatedAt() time.Time { return d.createdAt }

// UpdatedAt is when it last changed.
func (d OutputDelivery) UpdatedAt() time.Time { return d.updatedAt }

// Due reports a pending delivery whose next attempt has come.
func (d OutputDelivery) Due(now time.Time) bool {
	return d.status == DeliveryPending && !now.Before(d.nextAttemptAt)
}

// Delivered records an attempt the target accepted.
func (d *OutputDelivery) Delivered(now time.Time) error {
	if err := d.attempt("deliver"); err != nil {
		return err
	}
	d.status, d.lastError, d.updatedAt = DeliveryDelivered, "", now
	return nil
}

// Retry records a failed attempt and when the next is due.
func (d *OutputDelivery) Retry(reason string, next time.Time, now time.Time) error {
	if reason == "" {
		return invalid("delivery error", "must not be empty")
	}
	if err := d.attempt("retry"); err != nil {
		return err
	}
	d.lastError, d.nextAttemptAt, d.updatedAt = reason, next, now
	return nil
}

// GiveUp records the last failed attempt.
func (d *OutputDelivery) GiveUp(reason string, now time.Time) error {
	if reason == "" {
		return invalid("delivery error", "must not be empty")
	}
	if err := d.attempt("give up"); err != nil {
		return err
	}
	d.status, d.lastError, d.updatedAt = DeliveryFailed, reason, now
	return nil
}

// Skip ends a pending delivery without an attempt: its target is gone.
func (d *OutputDelivery) Skip(reason string, now time.Time) error {
	if d.status != DeliveryPending {
		return StateError{Subject: "output delivery " + d.id.String(), Current: d.status.String(), Attempted: "skip"}
	}
	d.status, d.lastError, d.updatedAt = DeliverySkipped, reason, now
	return nil
}

func (d *OutputDelivery) attempt(attempted string) error {
	if d.status != DeliveryPending {
		return StateError{Subject: "output delivery " + d.id.String(), Current: d.status.String(), Attempted: attempted}
	}
	d.attempts++
	return nil
}

// Snapshot returns the delivery as plain data.
func (d OutputDelivery) Snapshot() OutputDeliverySnapshot {
	return OutputDeliverySnapshot{
		ID: d.id.String(), Output: d.output.String(), Kind: d.kind.String(), IncidentID: d.incident.String(), TenantID: d.tenant.String(),
		EventID: d.eventID.String(), Event: d.event.String(), Payload: d.Payload(), Status: d.status.String(), Attempts: d.attempts,
		NextAttemptAt: d.nextAttemptAt, LastError: d.lastError, CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
	}
}

// RestoreOutputDelivery rebuilds a delivery from a snapshot.
func RestoreOutputDelivery(s OutputDeliverySnapshot) (OutputDelivery, error) {
	id, err := NewDeliveryID(s.ID)
	if err != nil {
		return OutputDelivery{}, err
	}
	output, err := NewOutputName(s.Output)
	if err != nil {
		return OutputDelivery{}, err
	}
	kind, err := ParseOutputKind(s.Kind)
	if err != nil {
		return OutputDelivery{}, err
	}
	incident, err := NewIncidentID(s.IncidentID)
	if err != nil {
		return OutputDelivery{}, err
	}
	tenant, err := NewTenantID(s.TenantID)
	if err != nil {
		return OutputDelivery{}, err
	}
	eventID, err := NewEventID(s.EventID)
	if err != nil {
		return OutputDelivery{}, err
	}
	event, err := ParseEventType(s.Event)
	if err != nil {
		return OutputDelivery{}, err
	}
	status, err := ParseDeliveryStatus(s.Status)
	if err != nil {
		return OutputDelivery{}, err
	}
	if s.Attempts < 0 {
		return OutputDelivery{}, invalid("delivery attempts", "must not be negative")
	}
	if s.UpdatedAt.IsZero() {
		return OutputDelivery{}, invalid("updated at", "must not be zero")
	}
	d, err := newOutputDelivery(id, output, kind, incident, tenant, eventID, event, s.Payload, s.CreatedAt)
	if err != nil {
		return OutputDelivery{}, err
	}
	d.status, d.attempts, d.nextAttemptAt, d.lastError, d.updatedAt = status, s.Attempts, s.NextAttemptAt, s.LastError, s.UpdatedAt
	return d, nil
}
