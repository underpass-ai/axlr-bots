package unixapi

// TaskRequest asks for a task on the bot named in the path (POST
// /v1/bots/{id}/tasks). The key comes from the Idempotency-Key header
// or the body; the header wins when both are given.
type TaskRequest struct {
	Objective      string `json:"objective"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	CorrelationID  string `json:"correlationId,omitempty"`
	// Priority is urgent for a task that goes first and cancels the
	// bot's work in progress; absent or normal waits its turn.
	Priority string `json:"priority,omitempty"`
}
