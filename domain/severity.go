package domain

// Severity says how much an event matters to people: info goes to the
// history, warning also to the panel and the phone, critical also to the
// emitting bot's notification targets. The kind of the event's name
// fixes it (KindOf). A teammate hears the team's broadcasts of warning
// and above.
type Severity string

const (
	// SeverityInfo is a fact for the record.
	SeverityInfo Severity = "info"
	// SeverityWarning is something people should see now.
	SeverityWarning Severity = "warning"
	// SeverityCritical is something that should reach people wherever
	// they are.
	SeverityCritical Severity = "critical"
)

// rank orders the severities.
func (s Severity) rank() int {
	switch s {
	case SeverityWarning:
		return 1
	case SeverityCritical:
		return 2
	}
	return 0
}

// AtLeast reports whether the severity is other or above it.
func (s Severity) AtLeast(other Severity) bool { return s.rank() >= other.rank() }

func (s Severity) String() string { return string(s) }
