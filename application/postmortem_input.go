package application

import "time"

// PostmortemInput is the input of a postmortem's task: what the control
// plane knows of the incident, each field named on purpose. It holds no
// trigger, output or notification credential, no alert payload and no
// tool call's arguments or output; the diagnosis and the proposal are
// the bots' own words, and Untrusted names the fields that are; a
// runtime failure's text never enters
// (docs/architecture/incident-postmortem.md).
type PostmortemInput struct {
	Schema          string                    `json:"schema"`
	Untrusted       []string                  `json:"untrusted"`
	IncidentID      string                    `json:"incidentId"`
	Title           string                    `json:"title"`
	Severity        string                    `json:"severity"`
	Source          string                    `json:"source"`
	Namespaces      []string                  `json:"namespaces,omitempty"`
	Cycle           int                       `json:"cycle"`
	OpenedAt        time.Time                 `json:"openedAt"`
	ResolvedAt      time.Time                 `json:"resolvedAt"`
	DurationSeconds int64                     `json:"durationSeconds"`
	Reason          string                    `json:"reason,omitempty"`
	Diagnosis       string                    `json:"diagnosis,omitempty"`
	Proposal        string                    `json:"proposal,omitempty"`
	Timeline        []PostmortemTimelineEntry `json:"timeline"`
	Decisions       []PostmortemDecision      `json:"decisions"`
	Verdicts        []PostmortemVerdict       `json:"verdicts"`
	CostUSD         float64                   `json:"costUsd"`
	CostUnknown     bool                      `json:"costUnknown,omitempty"`
	Review          *PostmortemReview         `json:"review,omitempty"`
}

// PostmortemInputSchema names the shape of PostmortemInput.
const PostmortemInputSchema = "axlr-bots.incident-postmortem-input/v1"
