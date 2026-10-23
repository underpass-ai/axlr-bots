package domain

// EventID identifies a event.
type EventID string

// NewEventID validates a event identifier.
func NewEventID(raw string) (EventID, error) {
	value, err := validateIdentifier("event id", raw)
	if err != nil {
		return "", err
	}
	return EventID(value), nil
}

func (id EventID) String() string { return string(id) }
