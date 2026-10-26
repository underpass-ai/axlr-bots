package nodeproto

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// EmittedMessage is the control plane's answer to an emit: the event,
// who got a task and who was skipped, or the refusal.
type EmittedMessage struct {
	EventID      string                      `json:"eventId,omitempty"`
	Name         string                      `json:"name"`
	Severity     string                      `json:"severity,omitempty"`
	Deliveries   []application.EventDelivery `json:"deliveries,omitempty"`
	Skipped      []application.EventSkip     `json:"skipped,omitempty"`
	Deduplicated bool                        `json:"deduplicated,omitempty"`
	Refused      *domain.EventRefusal        `json:"refused,omitempty"`
}
