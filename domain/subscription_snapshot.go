package domain

import "time"

// SubscriptionSnapshot is one topic a bot listens to, as plain data: the
// name or pattern, the mode (absent: by the event's severity), whom from
// (absent: anyone in the team), whether a warning it hears is urgent,
// the objective template of the task it makes, whether the team takes
// what it hears (deliver), and, for a bot's ask, its status and who
// asked, why and when. Team names another team the topic is heard from
// (docs/architecture/team-topics.md); absent: the bot's own teams.
// Expects lists the final events a task the subscription makes must emit,
// one per event it carried, with the carried event's data.id
// (docs/architecture/team-subscriptions.md, "Done is a contract");
// absent: a task ends as its run ends.
type SubscriptionSnapshot struct {
	Topic     string     `json:"topic"`
	Team      string     `json:"team,omitempty"`
	Mode      string     `json:"mode,omitempty"`
	From      []string   `json:"from,omitempty"`
	Urgent    bool       `json:"urgent,omitempty"`
	Objective string     `json:"objective,omitempty"`
	Deliver   string     `json:"deliver,omitempty"`
	Expects   []string   `json:"expects,omitempty"`
	Status    string     `json:"status,omitempty"`
	AskedBy   string     `json:"askedBy,omitempty"`
	Why       string     `json:"why,omitempty"`
	AskedAt   *time.Time `json:"askedAt,omitempty"`
}
