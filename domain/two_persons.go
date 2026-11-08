package domain

import "slices"

// maxTwoPersonsEntries bounds each list of twoPersons.
const maxTwoPersonsEntries = 32

// TwoPersons says which incidents a bot's call waits for two different
// people for: those of the namespaces listed, or of the severities
// listed (either list may be given, and both; one that matches is
// enough). It reads the incident's own namespace and severity, so it
// applies to the tasks of an incident only: a task of no incident needs
// one approval (decided 8 November 2026, docs/architecture/approvers.md).
type TwoPersons struct {
	namespaces []string
	severities []IncidentSeverity
}

// NewTwoPersons validates the setting: at least one entry, each
// namespace an identifier and each severity sev1 to sev4, no repeats.
func NewTwoPersons(s TwoPersonsSnapshot) (TwoPersons, error) {
	if len(s.Namespaces) == 0 && len(s.Severities) == 0 {
		return TwoPersons{}, invalid("approvals twoPersons", "names namespaces, severities or both")
	}
	if len(s.Namespaces) > maxTwoPersonsEntries || len(s.Severities) > maxTwoPersonsEntries {
		return TwoPersons{}, invalid("approvals twoPersons", "takes at most 32 namespaces and 32 severities")
	}
	var t TwoPersons
	for _, raw := range s.Namespaces {
		namespace, err := validateIdentifier("approvals twoPersons namespace", raw)
		if err != nil {
			return TwoPersons{}, err
		}
		if slices.Contains(t.namespaces, namespace) {
			return TwoPersons{}, invalid("approvals twoPersons", "repeat the namespace "+namespace)
		}
		t.namespaces = append(t.namespaces, namespace)
	}
	for _, raw := range s.Severities {
		if raw == "" {
			// ParseIncidentSeverity reads empty as sev3: here it is a mistake.
			return TwoPersons{}, invalid("approvals twoPersons severity", "must be sev1, sev2, sev3 or sev4")
		}
		severity, err := ParseIncidentSeverity(raw)
		if err != nil {
			return TwoPersons{}, invalid("approvals twoPersons severity", "must be sev1, sev2, sev3 or sev4")
		}
		if slices.Contains(t.severities, severity) {
			return TwoPersons{}, invalid("approvals twoPersons", "repeat the severity "+raw)
		}
		t.severities = append(t.severities, severity)
	}
	return t, nil
}

// Set reports a setting that names anything: the zero value names
// nothing and asks for two persons nowhere.
func (t TwoPersons) Set() bool { return len(t.namespaces) > 0 || len(t.severities) > 0 }

// Applies reports whether an incident of the namespace and severity
// waits for two persons. An empty namespace matches no namespace and an
// empty severity no severity: a task of no incident never matches.
func (t TwoPersons) Applies(namespace string, severity IncidentSeverity) bool {
	if namespace != "" && slices.Contains(t.namespaces, namespace) {
		return true
	}
	return severity != "" && slices.Contains(t.severities, severity)
}

// Snapshot returns the setting as plain data; nil when it names nothing.
func (t TwoPersons) Snapshot() *TwoPersonsSnapshot {
	if !t.Set() {
		return nil
	}
	s := &TwoPersonsSnapshot{Namespaces: append([]string(nil), t.namespaces...)}
	for _, severity := range t.severities {
		s.Severities = append(s.Severities, severity.String())
	}
	return s
}
