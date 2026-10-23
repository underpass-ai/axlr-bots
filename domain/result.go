package domain

// Result is what an execution produced: the runtime's answer, the session
// that holds the full record, the artifacts it left, the tool calls it
// made and, when it stopped early, the decision it waits for.
type Result struct {
	Answer          string           `json:"answer"`
	SessionID       string           `json:"sessionId,omitempty"`
	Artifacts       []string         `json:"artifacts,omitempty"`
	ToolCalls       []ToolCall       `json:"toolCalls,omitempty"`
	PendingDecision *PendingDecision `json:"pendingDecision,omitempty"`
}
