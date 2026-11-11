package application

import "github.com/underpass-ai/axlr-bots/domain"

// WatchDecided is what came of a watcher's decision: the question's status
// now, and whether it was abandoned (with the reason the asker hears).
type WatchDecided struct {
	Question  domain.TaskID
	Choice    string
	Status    domain.TaskStatus
	Abandoned bool
	Reason    string
}
