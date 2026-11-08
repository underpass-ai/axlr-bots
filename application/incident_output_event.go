package application

import "time"

// IncidentOutputSchema names the shape of an incident output's body; a
// receiver checks it before the fields.
const IncidentOutputSchema = "axlr-bots.incident-event/v1"

// MaxOutputExcerpt bounds the diagnosis and the proposal an output
// carries; GET /v1/incidents/{id} has them whole.
const MaxOutputExcerpt = 2 << 10

// IncidentOutputEvent is what a webhook or a command output receives on
// an incident event: the incident where it stands after the event, the
// diagnosis and the proposal cut to MaxOutputExcerpt, the cost, and the
// links. DeliveryID is the same on every attempt, so a receiver can drop
// a repeat. NeedsYou is what GET /v1/incidents/{id} answers: the
// incident's current task waits for a person's approval where the
// incident stands.
type IncidentOutputEvent struct {
	Schema             string              `json:"schema"`
	DeliveryID         string              `json:"deliveryId"`
	Event              string              `json:"event"`
	EventID            string              `json:"eventId"`
	Target             string              `json:"target"`
	OccurredAt         time.Time           `json:"occurredAt"`
	IncidentID         string              `json:"incidentId"`
	TenantID           string              `json:"tenantId"`
	BotID              string              `json:"botId"`
	BotName            string              `json:"botName"`
	TaskID             string              `json:"taskId,omitempty"`
	Title              string              `json:"title"`
	Severity           string              `json:"severity"`
	Status             string              `json:"status"`
	Cycle              int                 `json:"cycle"`
	GroupKey           string              `json:"groupKey"`
	Source             string              `json:"source"`
	Diagnosis          string              `json:"diagnosis,omitempty"`
	DiagnosisTruncated bool                `json:"diagnosisTruncated,omitempty"`
	Proposal           string              `json:"proposal,omitempty"`
	ProposalTruncated  bool                `json:"proposalTruncated,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	NeedsYou           bool                `json:"needsYou"`
	CostUSD            float64             `json:"costUsd"`
	CostUnknown        bool                `json:"costUnknown,omitempty"`
	OpenedAt           time.Time           `json:"openedAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
	ResolvedAt         *time.Time          `json:"resolvedAt,omitempty"`
	DurationSeconds    int64               `json:"durationSeconds,omitempty"`
	Links              IncidentOutputLinks `json:"links"`
}

// IncidentOutputLinks are where to read the incident: the operator API's
// route (Path always, URL when the control plane knows its operator
// address, serve --api-url) and the panel's page when a panel URL is
// known (serve --panel-url, or the target's own panelUrl).
type IncidentOutputLinks struct {
	Incident ApprovalAction `json:"incident"`
	Panel    string         `json:"panel,omitempty"`
}
