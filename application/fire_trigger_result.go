package application

import "github.com/underpass-ai/axlr-bots/domain"

// FireTriggerResult is the task a fire names, whether this fire created
// it, and the key the fire was deduplicated by.
type FireTriggerResult struct {
	Task           domain.Task
	Created        bool
	Trigger        domain.TriggerRef
	IdempotencyKey domain.IdempotencyKey
}
