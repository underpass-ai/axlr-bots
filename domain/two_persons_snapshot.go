package domain

// TwoPersonsSnapshot is a bot's twoPersons setting as plain data: the
// incident namespaces and the incident severities whose calls wait for
// two different people.
type TwoPersonsSnapshot struct {
	Namespaces []string `json:"namespaces,omitempty"`
	Severities []string `json:"severities,omitempty"`
}
