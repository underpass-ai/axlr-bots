package domain

// IncidentSeverity ranks an incident, sev1 the worst: the alert's
// severity label, or what triage says.
type IncidentSeverity string

const (
	// Sev1 is an outage: a person is paged whatever the bots do.
	Sev1 IncidentSeverity = "sev1"
	// Sev2 is a degraded service.
	Sev2 IncidentSeverity = "sev2"
	// Sev3 is a fault without visible impact yet: the default.
	Sev3 IncidentSeverity = "sev3"
	// Sev4 is a warning for the record.
	Sev4 IncidentSeverity = "sev4"
)

// ParseIncidentSeverity validates a severity; empty is sev3.
func ParseIncidentSeverity(raw string) (IncidentSeverity, error) {
	switch IncidentSeverity(raw) {
	case "":
		return Sev3, nil
	case Sev1, Sev2, Sev3, Sev4:
		return IncidentSeverity(raw), nil
	}
	return "", invalid("severity", "must be sev1, sev2, sev3 or sev4")
}

func (s IncidentSeverity) String() string { return string(s) }
