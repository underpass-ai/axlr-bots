package application

import "github.com/underpass-ai/axlr-bots/domain"

// DeleteBotResult is what deleting a bot did: the bot as it was, its
// tasks and the delegated children it asked for that were cancelled,
// its incidents resolved, its timers removed, the notifications of its
// approvals dropped, and the other bots that still name it.
type DeleteBotResult struct {
	Bot                  domain.Bot
	Cancelled            []domain.TaskID
	ChildrenCancelled    []domain.TaskID
	IncidentsResolved    []domain.IncidentID
	TimersRemoved        int
	NotificationsDropped int
	NamedBy              []BotReference
}
