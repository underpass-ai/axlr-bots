package application

import "github.com/underpass-ai/axlr-bots/domain"

// QuestionAnswer is what the control plane tells the node about a
// question: the refusal, or the question's task, the bot picked to answer
// it and the task's state. Created is false when the same ask had already
// made it.
type QuestionAnswer struct {
	Refused *domain.QuestionRefusal
	TaskID  domain.TaskID
	BotID   domain.BotID
	Status  domain.TaskStatus
	Created bool
}
