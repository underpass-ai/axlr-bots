package domain

import (
	"slices"
	"strconv"
)

// Limits of what a task records for its subscription's expects.
const (
	// MaxCarriedEvents is the most events one task carries: a Pending
	// task that carries as many takes no more as notes, and the next event
	// makes a task of its own.
	MaxCarriedEvents = 32
	// MaxRedeliveries is how many times the control plane gives an event
	// without its final event again; after that it publishes
	// warn.incomplete.
	MaxRedeliveries = 2
	// maxCarriedMessage bounds the message a carried event keeps.
	maxCarriedMessage = 280
)

// CarriedEvent is one event a task carries for its subscription's
// expects: the event that made the task or one coalesced into it as a
// note. ID is its data.id (empty: it had none); Redeliveries how many
// times the control plane gave it again, and Tasks the earlier tasks that
// carried it, oldest first.
type CarriedEvent struct {
	EventID      string   `json:"eventId"`
	Name         string   `json:"name"`
	ID           string   `json:"id,omitempty"`
	From         string   `json:"from,omitempty"`
	Message      string   `json:"message,omitempty"`
	Redeliveries int      `json:"redeliveries,omitempty"`
	Tasks        []string `json:"tasks,omitempty"`
}

// FinalEvent is one event the task emitted whose name fits its expects,
// with its data.id.
type FinalEvent struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// TaskExpectsSnapshot is what a task records for its subscription's
// expects (docs/architecture/team-subscriptions.md, "Done is a
// contract"): the names or patterns a final event fits, the events it
// carried and the finals it emitted.
type TaskExpectsSnapshot struct {
	Names   []string       `json:"names"`
	Carried []CarriedEvent `json:"carried,omitempty"`
	Finals  []FinalEvent   `json:"finals,omitempty"`
}

// taskExpects is the task's record for its subscription's expects.
type taskExpects struct {
	names   []EventPattern
	carried []CarriedEvent
	finals  []FinalEvent
}

// DataID reads an event's data.id as the contract compares it: a string
// as it is, a JSON number in its shortest form; anything else is none.
func DataID(data map[string]any) string {
	switch id := data["id"].(type) {
	case string:
		return id
	case float64:
		return strconv.FormatFloat(id, 'f', -1, 64)
	}
	return ""
}

// Expect records the final events the task must emit, from its
// subscription, before the task is first saved; none records nothing.
func (t *Task) Expect(names []EventPattern) {
	if len(names) == 0 {
		return
	}
	t.expects = &taskExpects{names: append([]EventPattern(nil), names...)}
}

// Expects are the final events the task must emit; empty when its
// subscription expects none.
func (t Task) Expects() []EventPattern {
	if t.expects == nil {
		return nil
	}
	return append([]EventPattern(nil), t.expects.names...)
}

// CanCarry reports whether the task, Pending and not started, takes one
// more event to carry: it expects finals and carries fewer than
// MaxCarriedEvents.
func (t Task) CanCarry() bool {
	return t.expects != nil && t.status == TaskPending && t.CurrentExecutionID() == "" && len(t.expects.carried) < MaxCarriedEvents
}

// Carry records an event the task carries: the one that made it, or one
// coalesced into it. The same event twice is one. False when the task
// expects nothing, has started or carries its most.
func (t *Task) Carry(event CarriedEvent) (bool, error) {
	if !t.CanCarry() {
		return false, nil
	}
	if slices.ContainsFunc(t.expects.carried, func(c CarriedEvent) bool { return c.EventID == event.EventID }) {
		return false, nil
	}
	if runes := []rune(event.Message); len(runes) > maxCarriedMessage {
		event.Message = string(runes[:maxCarriedMessage])
	}
	event.Tasks = append([]string(nil), event.Tasks...)
	if err := event.validate(); err != nil {
		return false, err
	}
	t.expects.carried = append(t.expects.carried, event)
	return true, nil
}

// Carried lists the events the task carries, oldest first.
func (t Task) Carried() []CarriedEvent {
	if t.expects == nil {
		return nil
	}
	return copyCarried(t.expects.carried)
}

// RecordFinal records an event the Running task emitted when its name
// fits the task's expects and it settles a carried event not settled
// yet: one whose data.id it bears, or, for a carried event without a
// data.id, any. False when it records nothing.
func (t *Task) RecordFinal(name EventName, id string) bool {
	if t.expects == nil || t.status != TaskRunning || !t.fits(name) {
		return false
	}
	settles := false
	for _, carried := range t.expects.carried {
		if carried.ID == id && !t.settled(carried) || carried.ID == "" && len(t.expects.finals) == 0 {
			settles = true
		}
	}
	if !settles {
		return false
	}
	t.expects.finals = append(t.expects.finals, FinalEvent{Name: name.String(), ID: id})
	return true
}

// Finals lists the final events the task emitted, oldest first.
func (t Task) Finals() []FinalEvent {
	if t.expects == nil {
		return nil
	}
	return append([]FinalEvent(nil), t.expects.finals...)
}

// Missing lists the carried events without their final event, oldest
// first: one with a data.id needs a final bearing it; one without, any
// final.
func (t Task) Missing() []CarriedEvent {
	if t.expects == nil {
		return nil
	}
	var missing []CarriedEvent
	for _, carried := range t.expects.carried {
		if !t.settled(carried) {
			missing = append(missing, carried)
		}
	}
	return copyCarried(missing)
}

func (t Task) fits(name EventName) bool {
	return slices.ContainsFunc(t.expects.names, func(p EventPattern) bool { return p.Matches(name) })
}

func (t Task) settled(carried CarriedEvent) bool {
	if carried.ID == "" {
		return len(t.expects.finals) > 0
	}
	return slices.ContainsFunc(t.expects.finals, func(f FinalEvent) bool { return f.ID == carried.ID })
}

func (t Task) expectsSnapshot() *TaskExpectsSnapshot {
	if t.expects == nil {
		return nil
	}
	out := &TaskExpectsSnapshot{Carried: copyCarried(t.expects.carried), Finals: append([]FinalEvent(nil), t.expects.finals...)}
	for _, name := range t.expects.names {
		out.Names = append(out.Names, name.String())
	}
	return out
}

func restoreTaskExpects(s *TaskExpectsSnapshot) (*taskExpects, error) {
	if s == nil {
		return nil, nil
	}
	if len(s.Names) == 0 || len(s.Names) > MaxSubscriptionExpects {
		return nil, invalid("expects names", "must name 1 to 8 final events")
	}
	if len(s.Carried) > MaxCarriedEvents || len(s.Finals) > MaxCarriedEvents+1 {
		return nil, invalid("expects", "carries at most 32 events")
	}
	out := &taskExpects{carried: copyCarried(s.Carried), finals: append([]FinalEvent(nil), s.Finals...)}
	for _, raw := range s.Names {
		pattern, err := NewEventPattern(raw)
		if err != nil {
			return nil, invalid("expects names", err.Error())
		}
		out.names = append(out.names, pattern)
	}
	for _, carried := range out.carried {
		if err := carried.validate(); err != nil {
			return nil, err
		}
	}
	for _, final := range out.finals {
		if _, err := NewEventName(final.Name); err != nil {
			return nil, invalid("expects finals", err.Error())
		}
	}
	return out, nil
}

func (c CarriedEvent) validate() error {
	if _, err := NewEventID(c.EventID); err != nil {
		return invalid("carried event", err.Error())
	}
	if _, err := NewEventName(c.Name); err != nil {
		return invalid("carried event", err.Error())
	}
	if c.Redeliveries < 0 || c.Redeliveries > MaxRedeliveries || len(c.Tasks) > MaxRedeliveries {
		return invalid("carried event", "is given again at most 2 times")
	}
	for _, raw := range c.Tasks {
		if _, err := NewTaskID(raw); err != nil {
			return invalid("carried event", err.Error())
		}
	}
	return nil
}

func copyCarried(list []CarriedEvent) []CarriedEvent {
	if list == nil {
		return nil
	}
	out := make([]CarriedEvent, len(list))
	for i, c := range list {
		c.Tasks = append([]string(nil), c.Tasks...)
		out[i] = c
	}
	return out
}
