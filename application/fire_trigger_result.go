package application

import "github.com/underpass-ai/axlr-bots/domain"

// FireTriggerResult is the task a fire names, whether this fire created
// it, and the key the fire was deduplicated by.
//
// An alertmanager fire names the incident the group has and what the
// fire did to it (Action: opened, repeated, reopened, resolved or
// ignored); its Task is the triage task started, nil for a repeat, a
// resolution or a group nobody had. A cloudwatch fire does the same for
// an alarm, and answers confirmed for a subscription it confirmed, ignored
// for one left to a person and for what opens nothing.
type FireTriggerResult struct {
	Task           *domain.Task
	Created        bool
	Trigger        domain.TriggerRef
	IdempotencyKey domain.IdempotencyKey
	Incident       *domain.Incident
	Action         string
}
