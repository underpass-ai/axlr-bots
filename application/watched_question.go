package application

import "github.com/underpass-ai/axlr-bots/domain"

// WatchedQuestion is what a team's Jev watcher is put next: the view of
// the next open question its team asked, or, when Task is set, of one of
// its team's own running tasks; and the cursor that names it for the next
// call; Done when the round has no more.
type WatchedQuestion struct {
	Done bool
	// Team is the asking team: the watcher's own.
	Team domain.TeamName
	View QuestionView
	// Task is the view of a team task the round judges stuck or not;
	// nil for a question.
	Task   *TaskWatchView
	Cursor domain.TaskID
}
