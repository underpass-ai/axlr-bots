package application

import "github.com/underpass-ai/axlr-bots/domain"

// StopBotResult is the bot after Stop, the tasks it cancelled and the
// ones it could not.
type StopBotResult struct {
	Bot          domain.Bot
	Cancelled    []domain.TaskID
	NotCancelled []StopRefusal
}
