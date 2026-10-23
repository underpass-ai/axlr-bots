package domain

// PendingDecision is the tool call an execution stopped on because the
// run was not allowed to decide it. Resuming the runtime session with a
// decision continues the task.
type PendingDecision struct {
	Tool      string `json:"tool"`
	CallID    string `json:"callId"`
	SessionID string `json:"sessionId"`
}
