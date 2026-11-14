package application

import "github.com/underpass-ai/axlr-bots/domain"

// ActivityUpdate is one message of a watch: exactly one of the fields
// besides Cursor is set (Heartbeat true, for an idle stream). Cursor
// identifies this point of the stream; a watch resumed from it gets what
// changed after.
type ActivityUpdate struct {
	Cursor    string
	Snapshot  *TaskActivity
	Task      *domain.Task
	Execution *domain.Execution
	Event     *ActivityEvent
	Child     *ChildAttached
	Completed *ProgressCompletion
	Heartbeat bool
}
