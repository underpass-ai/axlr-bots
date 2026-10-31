package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// OpenIncidentResponse is what reporting an alert group returns: the
// incident it names, the triage task started for it (absent for a
// repeat), and whether this call created the incident or reopened it.
type OpenIncidentResponse struct {
	Incident domain.IncidentSnapshot `json:"incident"`
	Task     *domain.TaskSnapshot    `json:"task,omitempty"`
	Created  bool                    `json:"created"`
	Reopened bool                    `json:"reopened"`
}
