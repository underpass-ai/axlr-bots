package application

import "github.com/underpass-ai/axlr-bots/domain"

// ChildAttached is a delegated task that joined the tree, with what is
// already known of it.
type ChildAttached struct {
	ParentTaskID domain.TaskID
	Child        TaskActivity
}
