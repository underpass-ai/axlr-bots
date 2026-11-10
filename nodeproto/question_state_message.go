package nodeproto

import (
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// QuestionStateMessage is where a question stands, from the control
// plane's records: never its answer, which comes back through the channel.
// Zero times and empty strings are what does not apply.
type QuestionStateMessage struct {
	Question      string     `json:"question"`
	Team          string     `json:"team,omitempty"`
	AskedBy       string     `json:"askedBy"`
	Responder     string     `json:"responder"`
	Status        string     `json:"status"`
	AskedAt       time.Time  `json:"askedAt"`
	Deadline      time.Time  `json:"deadline"`
	Now           time.Time  `json:"now"`
	OpenTasks     int        `json:"openTasks"`
	Waiting       string     `json:"waiting,omitempty"`
	RunningSince  *time.Time `json:"runningSince,omitempty"`
	LastStep      string     `json:"lastStep,omitempty"`
	LastStepAt    *time.Time `json:"lastStepAt,omitempty"`
	ApprovalTool  string     `json:"approvalTool,omitempty"`
	ApprovalSince *time.Time `json:"approvalSince,omitempty"`
	Reason        string     `json:"reason,omitempty"`
}

// FromQuestionView renders a question's state as a message.
func FromQuestionView(v application.QuestionView) QuestionStateMessage {
	at := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	return QuestionStateMessage{
		Question: v.Question.String(), Team: v.Team.String(), AskedBy: v.AskedBy.String(), Responder: v.Responder.String(), Status: v.Status.String(),
		AskedAt: v.AskedAt, Deadline: v.Deadline, Now: v.Now, OpenTasks: v.OpenTasks, Waiting: v.Waiting,
		RunningSince: at(v.RunningSince), LastStep: v.LastStep, LastStepAt: at(v.LastStepAt), ApprovalTool: v.ApprovalTool, ApprovalSince: at(v.ApprovalSince), Reason: v.Reason,
	}
}

// ToQuestionView reads a question's state back.
func ToQuestionView(m QuestionStateMessage) (application.QuestionView, error) {
	status, err := domain.ParseTaskStatus(m.Status)
	if err != nil {
		return application.QuestionView{}, err
	}
	question, err := domain.NewTaskID(m.Question)
	if err != nil {
		return application.QuestionView{}, err
	}
	of := func(t *time.Time) time.Time {
		if t == nil {
			return time.Time{}
		}
		return *t
	}
	return application.QuestionView{
		Question: question, Team: domain.TeamName(m.Team), AskedBy: domain.BotID(m.AskedBy), Responder: domain.BotID(m.Responder), Status: status,
		AskedAt: m.AskedAt, Deadline: m.Deadline, Now: m.Now, OpenTasks: m.OpenTasks, Waiting: m.Waiting,
		RunningSince: of(m.RunningSince), LastStep: m.LastStep, LastStepAt: of(m.LastStepAt), ApprovalTool: m.ApprovalTool, ApprovalSince: of(m.ApprovalSince), Reason: m.Reason,
	}, nil
}
