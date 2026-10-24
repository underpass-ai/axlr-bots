package main

// reconcileOutput is what task reconcile prints: the ids of the tasks it
// failed as lost and of those still within their grace period.
type reconcileOutput struct {
	Failed  []string `json:"failed"`
	Skipped []string `json:"skipped"`
}
