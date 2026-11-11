package nodeproto

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// WatchedMessage is the next question of a watcher's round, with the
// cursor to send back, or done when the round has no more.
type WatchedMessage struct {
	Done     bool                  `json:"done,omitempty"`
	Team     string                `json:"team"`
	Cursor   string                `json:"cursor,omitempty"`
	Question *QuestionStateMessage `json:"question,omitempty"`
}

// FromWatched renders the next question as a message.
func FromWatched(w application.WatchedQuestion) WatchedMessage {
	message := WatchedMessage{Done: w.Done, Team: w.Team.String()}
	if !w.Done {
		view := FromQuestionView(w.View)
		message.Cursor, message.Question = w.Cursor.String(), &view
	}
	return message
}

// ToWatched reads the next question back.
func ToWatched(m WatchedMessage) (application.WatchedQuestion, error) {
	watched := application.WatchedQuestion{Done: m.Done, Team: domain.TeamName(m.Team)}
	if m.Done {
		return watched, nil
	}
	if m.Question == nil {
		return application.WatchedQuestion{}, domain.ValidationError{Field: "question", Reason: "a round that is not done names its next question"}
	}
	view, err := ToQuestionView(*m.Question)
	if err != nil {
		return application.WatchedQuestion{}, err
	}
	cursor, err := domain.NewTaskID(m.Cursor)
	if err != nil {
		return application.WatchedQuestion{}, err
	}
	watched.View, watched.Cursor = view, cursor
	return watched, nil
}
