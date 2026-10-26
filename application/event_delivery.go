package application

// EventDelivery is a teammate that got a task from an event: the bot,
// its task, the trigger it heard the event by, and false in Created when
// the same event had made the task before.
type EventDelivery struct {
	BotID   string `json:"botId"`
	TaskID  string `json:"taskId"`
	Trigger string `json:"trigger"`
	Created bool   `json:"created"`
}
