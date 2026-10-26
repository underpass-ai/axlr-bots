package application

// EventSkip is a teammate an event did not reach, and why: it does not
// listen to the event, it takes no task now, or the chain is too deep or
// would loop through it.
type EventSkip struct {
	BotID   string `json:"botId"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
