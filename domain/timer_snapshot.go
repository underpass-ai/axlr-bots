package domain

import "time"

// TimerSnapshot is a timer as plain data.
type TimerSnapshot struct {
	BotID           string     `json:"botId"`
	Trigger         string     `json:"trigger"`
	Schedule        string     `json:"schedule"`
	NextFireAt      time.Time  `json:"nextFireAt"`
	LastFiredAt     *time.Time `json:"lastFiredAt,omitempty"`
	LastScheduledAt *time.Time `json:"lastScheduledAt,omitempty"`
	LastTaskID      string     `json:"lastTaskId,omitempty"`
}
