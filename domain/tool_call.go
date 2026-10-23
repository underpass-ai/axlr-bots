package domain

// ToolCall is one tool the runtime asked for during an execution, as the
// result reports it: the tool, the call id, the policy's decision
// (approve, auto_approve, deny) and the call's status (pending, denied,
// completed, failed, uncertain).
type ToolCall struct {
	Name     string `json:"name"`
	CallID   string `json:"callId"`
	Decision string `json:"decision,omitempty"`
	Status   string `json:"status,omitempty"`
}
