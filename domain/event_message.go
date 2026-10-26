package domain

import "strings"

// MaxEventMessageLength bounds an event's message.
const MaxEventMessageLength = 280

// EventMessage is the one line an event carries for people and bots:
// what happened and why. Line breaks become spaces.
type EventMessage string

// NewEventMessage validates a message: 1 to 280 characters of text on
// one line.
func NewEventMessage(raw string) (EventMessage, error) {
	value, err := validateText("event message", strings.Join(strings.Fields(raw), " "), MaxEventMessageLength)
	if err != nil {
		return "", err
	}
	return EventMessage(value), nil
}

func (m EventMessage) String() string { return string(m) }
