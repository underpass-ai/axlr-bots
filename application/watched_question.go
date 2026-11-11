package application

import "github.com/underpass-ai/axlr-bots/domain"

// WatchedQuestion is what a team's Jev watcher is put next: the view of
// the next open question its team asked, and the cursor that names it for
// the next call; Done when the round has no more.
type WatchedQuestion struct {
	Done bool
	// Team is the asking team: the watcher's own.
	Team   domain.TeamName
	View   QuestionView
	Cursor domain.TaskID
}
