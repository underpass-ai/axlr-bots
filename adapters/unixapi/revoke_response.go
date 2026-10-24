package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// RevokeResponse is the revoked node and the tasks that failed with it.
type RevokeResponse struct {
	Node        domain.NodeSnapshot `json:"node"`
	FailedTasks []string            `json:"failedTasks"`
}
