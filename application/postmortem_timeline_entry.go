package application

import "time"

// PostmortemTimelineEntry is one line of an incident's timeline as a
// postmortem's task receives it: when, which event, of which task, phase
// and cycle, and only the fields that say what happened. Never an
// event's whole payload: a tool call's arguments are not a timeline's.
type PostmortemTimelineEntry struct {
	At       time.Time `json:"at"`
	Event    string    `json:"event"`
	TaskID   string    `json:"taskId,omitempty"`
	Phase    string    `json:"phase,omitempty"`
	Cycle    int       `json:"cycle,omitempty"`
	BotID    string    `json:"botId,omitempty"`
	Actor    string    `json:"actor,omitempty"`
	Decision string    `json:"decision,omitempty"`
	Tool     string    `json:"tool,omitempty"`
	Status   string    `json:"status,omitempty"`
	From     string    `json:"from,omitempty"`
	Verdict  string    `json:"verdict,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}
