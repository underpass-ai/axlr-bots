package application

import "github.com/underpass-ai/axlr-bots/domain"

// ActivityEvent is an event that joined a task's timeline in the tree.
type ActivityEvent struct {
	TaskID domain.TaskID
	Event  domain.Event
}
