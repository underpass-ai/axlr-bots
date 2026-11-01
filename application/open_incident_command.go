package application

// OpenIncidentCommand reports an alert group to the control plane: the
// bot that triages it, what the source groups the alerts by, and the
// alerts themselves. The same tenant and group key again, while the
// incident is open, is a repeat; soon after its resolution, a reopening.
type OpenIncidentCommand struct {
	Tenant       string   `json:"tenant"`
	BotID        string   `json:"botId"`
	GroupKey     string   `json:"groupKey"`
	Source       string   `json:"source,omitempty"`
	Title        string   `json:"title"`
	Severity     string   `json:"severity,omitempty"`
	Fingerprints []string `json:"fingerprints,omitempty"`
	// Objective, when given, is what the triage task asks for; absent,
	// the trigger's template or the default is rendered over the
	// incident.
	Objective string `json:"objective,omitempty"`
}
