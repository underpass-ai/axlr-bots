package application

import "time"

// TaskQuery selects the tasks of a list. Every field is optional: no
// bot means every bot, no status every status, a zero Since every
// creation time, and a Limit of zero DefaultTaskLimit.
type TaskQuery struct {
	BotID  string
	Status string
	Since  time.Time
	Limit  int
}
