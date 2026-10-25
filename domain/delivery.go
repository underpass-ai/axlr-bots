package domain

import (
	"bytes"
	"encoding/json"
	"time"
)

// Delivery is one notification of an approval request to one target:
// the payload, fixed when it was queued so every attempt sends the same
// bytes, how many attempts were made and when the next is due. It is
// durable, so a restart of the control plane resumes the retries.
type Delivery struct {
	id            DeliveryID
	botID         BotID
	taskID        TaskID
	executionID   ExecutionID
	callID        string
	target        string
	kind          NotifyKind
	payload       json.RawMessage
	status        DeliveryStatus
	attempts      int
	nextAttemptAt time.Time
	lastError     string
	createdAt     time.Time
	updatedAt     time.Time
}

// NewDelivery queues a delivery whose first attempt is due now.
func NewDelivery(id DeliveryID, bot BotID, task TaskID, execution ExecutionID, callID string, target NotifyTarget, payload json.RawMessage, now time.Time) (Delivery, error) {
	if now.IsZero() {
		return Delivery{}, invalid("created at", "must not be zero")
	}
	// Compact, so the bytes sent are the same however the payload was
	// stored: the signature is over them.
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		return Delivery{}, invalid("delivery payload", "must be JSON")
	}
	return Delivery{
		id: id, botID: bot, taskID: task, executionID: execution, callID: callID, target: target.Name(), kind: target.Kind(),
		payload: json.RawMessage(compact.Bytes()), status: DeliveryPending, nextAttemptAt: now, createdAt: now, updatedAt: now,
	}, nil
}

// ID is the delivery's identifier, sent with every attempt.
func (d Delivery) ID() DeliveryID { return d.id }

// BotID is the bot whose approval request it carries.
func (d Delivery) BotID() BotID { return d.botID }

// TaskID is the waiting task.
func (d Delivery) TaskID() TaskID { return d.taskID }

// ExecutionID is the execution that stopped on the call.
func (d Delivery) ExecutionID() ExecutionID { return d.executionID }

// CallID is the pending call.
func (d Delivery) CallID() string { return d.callID }

// Target is the name of the bot's target it goes to.
func (d Delivery) Target() string { return d.target }

// Kind is the target's type when the delivery was queued.
func (d Delivery) Kind() NotifyKind { return d.kind }

// Payload is the JSON every attempt sends.
func (d Delivery) Payload() json.RawMessage { return append(json.RawMessage(nil), d.payload...) }

// Status is where the delivery is.
func (d Delivery) Status() DeliveryStatus { return d.status }

// Attempts counts the attempts made.
func (d Delivery) Attempts() int { return d.attempts }

// NextAttemptAt is when the next attempt is due.
func (d Delivery) NextAttemptAt() time.Time { return d.nextAttemptAt }

// LastError says why the last attempt failed.
func (d Delivery) LastError() string { return d.lastError }

// CreatedAt is when the delivery was queued.
func (d Delivery) CreatedAt() time.Time { return d.createdAt }

// UpdatedAt is when it last changed.
func (d Delivery) UpdatedAt() time.Time { return d.updatedAt }

// Due reports a pending delivery whose next attempt has come.
func (d Delivery) Due(now time.Time) bool {
	return d.status == DeliveryPending && !now.Before(d.nextAttemptAt)
}

// Delivered records an attempt the target accepted.
func (d *Delivery) Delivered(now time.Time) error {
	if err := d.attempt("deliver"); err != nil {
		return err
	}
	d.status, d.lastError, d.updatedAt = DeliveryDelivered, "", now
	return nil
}

// Retry records a failed attempt and when the next is due.
func (d *Delivery) Retry(reason string, next time.Time, now time.Time) error {
	if err := d.attempt("retry"); err != nil {
		return err
	}
	if reason == "" {
		return invalid("delivery error", "must not be empty")
	}
	d.lastError, d.nextAttemptAt, d.updatedAt = reason, next, now
	return nil
}

// GiveUp records the last failed attempt.
func (d *Delivery) GiveUp(reason string, now time.Time) error {
	if err := d.attempt("give up"); err != nil {
		return err
	}
	if reason == "" {
		return invalid("delivery error", "must not be empty")
	}
	d.status, d.lastError, d.updatedAt = DeliveryFailed, reason, now
	return nil
}

// Skip ends a pending delivery without an attempt: what it announces is
// over, or its target is gone.
func (d *Delivery) Skip(reason string, now time.Time) error {
	if d.status != DeliveryPending {
		return StateError{Subject: "delivery " + d.id.String(), Current: d.status.String(), Attempted: "skip"}
	}
	d.status, d.lastError, d.updatedAt = DeliverySkipped, reason, now
	return nil
}

func (d *Delivery) attempt(attempted string) error {
	if d.status != DeliveryPending {
		return StateError{Subject: "delivery " + d.id.String(), Current: d.status.String(), Attempted: attempted}
	}
	d.attempts++
	return nil
}

// Snapshot returns the delivery as plain data.
func (d Delivery) Snapshot() DeliverySnapshot {
	return DeliverySnapshot{
		ID: d.id.String(), BotID: d.botID.String(), TaskID: d.taskID.String(), ExecutionID: d.executionID.String(), CallID: d.callID,
		Target: d.target, Kind: d.kind.String(), Payload: d.Payload(), Status: d.status.String(), Attempts: d.attempts,
		NextAttemptAt: d.nextAttemptAt, LastError: d.lastError, CreatedAt: d.createdAt, UpdatedAt: d.updatedAt,
	}
}

// RestoreDelivery rebuilds a delivery from a snapshot.
func RestoreDelivery(s DeliverySnapshot) (Delivery, error) {
	id, err := NewDeliveryID(s.ID)
	if err != nil {
		return Delivery{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return Delivery{}, err
	}
	task, err := NewTaskID(s.TaskID)
	if err != nil {
		return Delivery{}, err
	}
	execution, err := NewExecutionID(s.ExecutionID)
	if err != nil {
		return Delivery{}, err
	}
	kind, err := ParseNotifyKind(s.Kind)
	if err != nil {
		return Delivery{}, err
	}
	status, err := ParseDeliveryStatus(s.Status)
	if err != nil {
		return Delivery{}, err
	}
	name, err := validateIdentifier("delivery target", s.Target)
	if err != nil {
		return Delivery{}, err
	}
	if s.Attempts < 0 {
		return Delivery{}, invalid("delivery attempts", "must not be negative")
	}
	if s.UpdatedAt.IsZero() {
		return Delivery{}, invalid("updated at", "must not be zero")
	}
	d, err := NewDelivery(id, bot, task, execution, s.CallID, NotifyTarget{kind: kind, name: name}, s.Payload, s.CreatedAt)
	if err != nil {
		return Delivery{}, err
	}
	d.status, d.attempts, d.nextAttemptAt, d.lastError, d.updatedAt = status, s.Attempts, s.NextAttemptAt, s.LastError, s.UpdatedAt
	return d, nil
}
