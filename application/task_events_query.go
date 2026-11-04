package application

import "github.com/underpass-ai/axlr-bots/domain"

// TaskEventsQuery asks for a task's timeline: the task, the id of the
// last event the caller has (empty: from the newest limit) and at most
// how many (100 when 0, at most 1000).
type TaskEventsQuery struct {
	ID    domain.TaskID
	After string
	Limit int
}
