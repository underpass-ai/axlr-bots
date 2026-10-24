package application

import "github.com/underpass-ai/axlr-bots/domain"

// RevokeNodeResult is the revoked node and the tasks whose executions
// on it were failed.
type RevokeNodeResult struct {
	Node        domain.Node
	FailedTasks []domain.TaskID
}
