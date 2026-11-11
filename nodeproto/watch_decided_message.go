package nodeproto

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// WatchDecidedMessage is what came of a watcher's decision: the
// question's status now and, when it was abandoned, the reason its asker
// hears.
type WatchDecidedMessage struct {
	Question  string `json:"question"`
	Choice    string `json:"choice"`
	Status    string `json:"status"`
	Abandoned bool   `json:"abandoned"`
	Reason    string `json:"reason,omitempty"`
}

// FromWatchDecided renders what came of the decision.
func FromWatchDecided(d application.WatchDecided) WatchDecidedMessage {
	return WatchDecidedMessage{Question: d.Question.String(), Choice: d.Choice, Status: d.Status.String(), Abandoned: d.Abandoned, Reason: d.Reason}
}

// ToWatchDecided reads it back.
func ToWatchDecided(m WatchDecidedMessage) (application.WatchDecided, error) {
	status, err := domain.ParseTaskStatus(m.Status)
	if err != nil {
		return application.WatchDecided{}, err
	}
	question, err := domain.NewTaskID(m.Question)
	if err != nil {
		return application.WatchDecided{}, err
	}
	return application.WatchDecided{Question: question, Choice: m.Choice, Status: status, Abandoned: m.Abandoned, Reason: m.Reason}, nil
}
