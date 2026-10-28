package application

// EventDelivery is a teammate that heard an event: the bot, what the
// event did to it (Mode), its task, the subscription it heard the event
// by (its topic, or team when addressed or the team's default), false
// in Created when the same event had made the task before, and whether
// the task is urgent: it stopped what the teammate was doing.
type EventDelivery struct {
	BotID   string `json:"botId"`
	Mode    string `json:"mode"`
	TaskID  string `json:"taskId"`
	Trigger string `json:"trigger"`
	Created bool   `json:"created"`
	Urgent  bool   `json:"urgent,omitempty"`
}
