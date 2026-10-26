package domain

import "strings"

// EventPattern matches event names: segments as in a name, where *
// stands for one segment and ** for any number of them, none included.
// ship.* matches ship.sighted, not ship; ship.** matches both, and **
// matches every name.
type EventPattern string

// AnyEvent matches every event name.
const AnyEvent EventPattern = "**"

// NewEventPattern validates a pattern.
func NewEventPattern(raw string) (EventPattern, error) {
	value := strings.TrimSpace(raw)
	if err := validateEventSegments("event pattern", value, true); err != nil {
		return "", err
	}
	return EventPattern(value), nil
}

// Matches reports whether the name fits the pattern.
func (p EventPattern) Matches(name EventName) bool {
	return matchSegments(strings.Split(string(p), "."), strings.Split(string(name), "."))
}

func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	switch pattern[0] {
	case "**":
		for i := 0; i <= len(name); i++ {
			if matchSegments(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	case "*":
		return len(name) > 0 && matchSegments(pattern[1:], name[1:])
	}
	return len(name) > 0 && pattern[0] == name[0] && matchSegments(pattern[1:], name[1:])
}

func (p EventPattern) String() string { return string(p) }
