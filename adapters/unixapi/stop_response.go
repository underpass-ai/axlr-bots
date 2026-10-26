package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// StopResponse is POST /v1/bots/{id}/stop's answer: the bot (paused),
// the tasks cancelled and the ones that could not be.
type StopResponse struct {
	Bot          domain.BotSnapshot        `json:"bot"`
	Cancelled    []string                  `json:"cancelled"`
	NotCancelled []application.StopRefusal `json:"notCancelled"`
}
