package taskactivity

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Update is one change of the live stream; exactly one field is set,
// and none at all is a heartbeat.
type Update struct {
	Snapshot          *Activity
	TaskChanged       *domain.TaskSnapshot
	ExecutionChanged  *domain.ExecutionSnapshot
	EventAppended     *EventAppended
	ChildAttached     *ChildAttached
	ProgressCompleted *ProgressCompleted
}

// EventAppended adds an event to a task's timeline.
type EventAppended struct {
	TaskID string
	Event  domain.Event
}

// ChildAttached adds a delegated task under its parent.
type ChildAttached struct {
	ParentTaskID string
	Child        Activity
}

// ProgressCompleted says an execution's progress was cut, or that its
// completion grew.
type ProgressCompleted struct {
	TaskID      string
	ExecutionID string
	Cut         *Cut
	Items       []application.ProgressItem
}

// Heartbeat reports whether the update only says nothing changed.
func (u Update) Heartbeat() bool {
	return u.Snapshot == nil && u.TaskChanged == nil && u.ExecutionChanged == nil && u.EventAppended == nil && u.ChildAttached == nil && u.ProgressCompleted == nil
}
