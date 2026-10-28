package application

import "time"

// GroupTopic is one topic of a team, derived: a name its events used or
// a pattern its members subscribe to or may emit, who publishes it (seen
// in the timeline, or declared by an emit rule), who listens and how,
// when it was last heard of and how often. NobodyListens says a name
// was published and no member listens to it now.
type GroupTopic struct {
	Name          string          `json:"name"`
	Publishers    []string        `json:"publishers"`
	Declared      []string        `json:"declaredPublishers,omitempty"`
	Listeners     []TopicListener `json:"listeners"`
	LastAt        *time.Time      `json:"lastAt,omitempty"`
	Count         int             `json:"count"`
	NobodyListens bool            `json:"nobodyListens,omitempty"`
}

// TopicListener is a member that listens to a topic: by which
// subscription, in which mode, and pending when it is a bot's ask.
type TopicListener struct {
	BotID  string `json:"botId"`
	Topic  string `json:"topic"`
	Mode   string `json:"mode,omitempty"`
	Status string `json:"status,omitempty"`
}
