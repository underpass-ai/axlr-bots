package domain

// Severity says how much an event matters to people: info goes to the
// history, warning also to the panel and the phone, critical also to the
// emitting bot's notification targets. A teammate hears the team's
// broadcasts of warning and above.
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

// ParseSeverity validates a severity; empty is info.
func ParseSeverity(raw string) (Severity, error) {
	switch Severity(raw) {
	case "":
		return SeverityInfo, nil
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return Severity(raw), nil
	}
	return "", invalid("severity", "must be info, warning or critical")
}

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

// UpTo lists the severities from info to s.
func (s Severity) UpTo() []Severity {
	all := []Severity{SeverityInfo, SeverityWarning, SeverityCritical}
	return all[:s.rank()+1]
}

func (s Severity) String() string { return string(s) }
