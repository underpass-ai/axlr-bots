package axlr

// resultSchema is the execution result the adapter understands.
const resultSchema = "axlr.execution-result/v1"

// resultObject is the execution result of the runtime contract.
type resultObject struct {
	Schema    string           `json:"schema"`
	Status    string           `json:"status"`
	SessionID string           `json:"session_id"`
	Sessions  []string         `json:"sessions"`
	Answer    string           `json:"answer"`
	Pending   *resultPending   `json:"pending,omitempty"`
	Error     string           `json:"error,omitempty"`
	ToolCalls []resultToolCall `json:"tool_calls"`
	Usage     resultUsage      `json:"usage"`
	Artifacts []resultArtifact `json:"artifacts"`
}
