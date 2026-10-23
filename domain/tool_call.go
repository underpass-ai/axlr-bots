package domain

// ToolCall is one tool the runtime asked for during an execution, as the
// result reports it: name, call id, the policy's decision and whether the
// call finished.
type ToolCall struct {
	Name      string `json:"name"`
	CallID    string `json:"callId"`
	Decision  string `json:"decision,omitempty"`
	Completed bool   `json:"completed"`
}
