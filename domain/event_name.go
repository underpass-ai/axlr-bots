package domain

import "strings"

// MaxEventNameLength bounds an event's name and a pattern of names.
const MaxEventNameLength = 64

// EventName names what a bot tells its team happened: dot-separated
// segments of lowercase letters, digits and '-', such as recall or
// ship.sighted.
type EventName string

// NewEventName validates an event's name.
func NewEventName(raw string) (EventName, error) {
	value := strings.TrimSpace(raw)
	if err := validateEventSegments("event name", value, false); err != nil {
		return "", err
	}
	return EventName(value), nil
}

func (n EventName) String() string { return string(n) }

// Canonical is the name as the bus reads it: the kindless recall the
// deployed pilots emit is stop.recall, for one release; any other name
// is itself.
func (n EventName) Canonical() EventName {
	if n == recallAlias {
		return EventName(KindStop.String() + "." + recallAlias)
	}
	return n
}

// validateEventSegments checks a name or, with wildcards, a pattern:
// segments of [a-z0-9-], or * and ** in a pattern.
func validateEventSegments(field, value string, wildcards bool) error {
	if value == "" {
		return invalid(field, "must not be empty")
	}
	if len(value) > MaxEventNameLength {
		return invalid(field, "must be at most 64 characters")
	}
	hint := "uses segments of lowercase letters, digits and '-' separated by '.'"
	if wildcards {
		hint += ", or * and **"
	}
	for _, segment := range strings.Split(value, ".") {
		if segment == "" {
			return invalid(field, "has an empty segment")
		}
		if wildcards && (segment == "*" || segment == "**") {
			continue
		}
		for _, r := range segment {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return invalid(field, hint)
			}
		}
	}
	return nil
}
