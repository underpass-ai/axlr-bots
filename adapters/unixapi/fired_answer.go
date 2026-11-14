package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// firedAnswer is what a webhook fire answers on the wire: whether it
// created the task, the task, the trigger and the key the fire was
// deduplicated by.
type firedAnswer struct {
	Created        bool                 `json:"created"`
	Task           *domain.TaskSnapshot `json:"task,omitempty"`
	Trigger        domain.TriggerRef    `json:"trigger"`
	IdempotencyKey string               `json:"idempotencyKey,omitempty"`
}

func newFiredAnswer(response application.FireResponse) firedAnswer {
	return firedAnswer(response)
}

func (a firedAnswer) response() application.FireResponse {
	return application.FireResponse(a)
}
