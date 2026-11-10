package nodeproto

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// QuestionMessage is the control plane's answer to a question, at once:
// the refusal, or the question's task, the bot picked to answer it and
// the task's state. The answer itself comes back later.
type QuestionMessage struct {
	Refused *domain.QuestionRefusal `json:"refused,omitempty"`
	TaskID  string                  `json:"taskId,omitempty"`
	BotID   string                  `json:"botId,omitempty"`
	Status  string                  `json:"status,omitempty"`
	Created bool                    `json:"created,omitempty"`
}

// FromQuestion renders a question's answer as a message.
func FromQuestion(a application.QuestionAnswer) QuestionMessage {
	if a.Refused != nil {
		return QuestionMessage{Refused: a.Refused}
	}
	return QuestionMessage{TaskID: a.TaskID.String(), BotID: a.BotID.String(), Status: a.Status.String(), Created: a.Created}
}

// ToQuestion reads a question's answer back.
func ToQuestion(m QuestionMessage) (application.QuestionAnswer, error) {
	if m.Refused != nil {
		return application.QuestionAnswer{Refused: m.Refused}, nil
	}
	task, err := domain.NewTaskID(m.TaskID)
	if err != nil {
		return application.QuestionAnswer{}, err
	}
	bot, err := domain.NewBotID(m.BotID)
	if err != nil {
		return application.QuestionAnswer{}, err
	}
	status, err := domain.ParseTaskStatus(m.Status)
	if err != nil {
		return application.QuestionAnswer{}, err
	}
	return application.QuestionAnswer{TaskID: task, BotID: bot, Status: status, Created: m.Created}, nil
}
