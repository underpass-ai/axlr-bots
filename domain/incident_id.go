package domain

// IncidentID identifies an incident.
type IncidentID string

// NewIncidentID validates an incident identifier.
func NewIncidentID(raw string) (IncidentID, error) {
	value, err := validateIdentifier("incident id", raw)
	if err != nil {
		return "", err
	}
	return IncidentID(value), nil
}

func (id IncidentID) String() string { return string(id) }
