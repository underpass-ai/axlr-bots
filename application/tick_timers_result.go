package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TickTimersResult is what one pass over the timers did: the fires, and
// the timers that could not fire (the pass goes on past them).
type TickTimersResult struct {
	Fired  []TimerFire
	Errors []TimerError
}

// TimerFire is one timer that fired: the slot, the task and whether the
// fire created it.
type TimerFire struct {
	BotID domain.BotID
	// Team names the team of a team's timer: BotID is the member picked.
	Team        domain.TeamName
	Trigger     string
	ScheduledAt time.Time
	TaskID      domain.TaskID
	Created     bool
}

// TimerError is one timer that could not fire or be saved.
type TimerError struct {
	BotID domain.BotID
	// Team names the team of a team's timer that could not fire.
	Team    domain.TeamName
	Trigger string
	Err     error
}
