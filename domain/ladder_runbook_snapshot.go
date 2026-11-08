package domain

// LadderRunbookSnapshot is one runbook of a ladder as plain data: its
// name (the alert name), the one write tool it may run alone and the
// namespaces it may run it in.
type LadderRunbookSnapshot struct {
	Name       string   `json:"name"`
	Tool       string   `json:"tool"`
	Namespaces []string `json:"namespaces"`
}
