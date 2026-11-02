package main

// reconcileOutput is what task reconcile prints: the ids of the tasks it
// failed as lost and of those still within their grace period, and the
// ids of the Running executions no task ran that it failed as orphans.
type reconcileOutput struct {
	Failed   []string `json:"failed"`
	Skipped  []string `json:"skipped"`
	Orphaned []string `json:"orphaned"`
}
