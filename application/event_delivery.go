package application

// EventDelivery is a teammate that heard an event: the bot, what the
// event did to it (Mode), its task (the new one, or the running one a
// message went into), the message, the subscription it heard the event
// by (its topic, or team when addressed or the team's default), false
// in Created when the same event had made the task before, and whether
// the task is urgent: it stopped what the teammate was doing.
type EventDelivery struct {
	BotID     string `json:"botId"`
	Mode      string `json:"mode"`
	TaskID    string `json:"taskId,omitempty"`
	MessageID string `json:"messageId,omitempty"`
	Trigger   string `json:"trigger"`
	Created   bool   `json:"created"`
	Urgent    bool   `json:"urgent,omitempty"`
	// Degraded says why a task or message became a note: intake_rate.
	Degraded string `json:"degraded,omitempty"`
	// CoalescedInto names the receiver's Pending task the event joined
	// as a note.
	CoalescedInto string `json:"coalescedInto,omitempty"`
	// Team names the team whose member took the event: one of the
	// hearers of a team subscription, picked by the control plane.
	Team string `json:"team,omitempty"`
	// Crossed says the event was published to another team, Team: its
	// task is made there, after the answer, so it names no bot or task.
	Crossed bool `json:"crossed,omitempty"`
}
