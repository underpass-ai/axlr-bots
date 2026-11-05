package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// DeleteResponse is DELETE /v1/bots/{id}'s answer: the bot as it was,
// the tasks and delegated children cancelled, the incidents resolved,
// the timers removed, the notifications dropped, and the other bots that
// still name it (their delegations and subscriptions are left as they
// are).
type DeleteResponse struct {
	Bot                  domain.BotSnapshot         `json:"bot"`
	Cancelled            []string                   `json:"cancelled"`
	ChildrenCancelled    []string                   `json:"childrenCancelled"`
	IncidentsResolved    []string                   `json:"incidentsResolved"`
	TimersRemoved        int                        `json:"timersRemoved"`
	NotificationsDropped int                        `json:"notificationsDropped"`
	NamedBy              []application.BotReference `json:"namedBy"`
}
