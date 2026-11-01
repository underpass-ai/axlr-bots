package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// FireResponse is what a fire returns: whether it created the task, the
// task, the trigger and the key the fire was deduplicated by. An
// alertmanager fire also names the incident and what the fire did to it
// (opened, repeated, reopened, resolved, ignored); its task is absent
// for a repeat, a resolution or a group nobody had.
type FireResponse struct {
	Created        bool                     `json:"created"`
	Task           *domain.TaskSnapshot     `json:"task,omitempty"`
	Trigger        domain.TriggerRef        `json:"trigger"`
	IdempotencyKey string                   `json:"idempotencyKey,omitempty"`
	Incident       *domain.IncidentSnapshot `json:"incident,omitempty"`
	Action         string                   `json:"action,omitempty"`
}
