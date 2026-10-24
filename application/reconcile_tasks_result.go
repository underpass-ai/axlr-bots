package application

import "github.com/underpass-ai/underpass-bots/domain"

// ReconcileTasksResult says which Running tasks were failed as lost and
// which were left alone because their execution is still within its
// grace period.
type ReconcileTasksResult struct {
	Failed  []domain.TaskID
	Skipped []domain.TaskID
}
