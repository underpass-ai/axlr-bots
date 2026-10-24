package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// FireResponse is what a fire returns: whether it created the task, the
// task, the trigger and the key the fire was deduplicated by.
type FireResponse struct {
	Created        bool                `json:"created"`
	Task           domain.TaskSnapshot `json:"task"`
	Trigger        domain.TriggerRef   `json:"trigger"`
	IdempotencyKey string              `json:"idempotencyKey"`
}
