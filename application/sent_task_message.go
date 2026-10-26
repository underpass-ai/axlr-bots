package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SentTaskMessage is a message the control plane took: the task and the
// execution it goes to, and when.
type SentTaskMessage struct {
	TaskID      domain.TaskID      `json:"taskId"`
	ExecutionID domain.ExecutionID `json:"executionId"`
	CreatedAt   time.Time          `json:"createdAt"`
}
