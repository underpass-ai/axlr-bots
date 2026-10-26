package application

// EventDelivery is a teammate that got a task from an event: the bot,
// its task, the trigger it heard the event by, false in Created when the
// same event had made the task before, and whether the task is urgent:
// it stopped what the teammate was doing.
type EventDelivery struct {
	BotID   string `json:"botId"`
	TaskID  string `json:"taskId"`
	Trigger string `json:"trigger"`
	Created bool   `json:"created"`
	Urgent  bool   `json:"urgent,omitempty"`
}
