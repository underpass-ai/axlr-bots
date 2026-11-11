package application

import "github.com/underpass-ai/axlr-bots/domain"

// WatchDecided is what came of a watcher's decision: the question's or
// task's status now, and whether it was abandoned or judged stuck and
// cancelled (with the reason).
type WatchDecided struct {
	Question  domain.TaskID
	Choice    string
	Status    domain.TaskStatus
	Abandoned bool
	Stuck     bool
	Reason    string
}
