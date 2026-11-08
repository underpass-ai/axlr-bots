package domain

import "encoding/json"

// PendingDecision is the tool call an execution stopped on because the
// run was not allowed to decide it: the tool, the call, the session that
// keeps it and the call's arguments as the runtime reported them, so the
// person who decides sees what would run. Resuming the runtime session
// with a decision continues the task.
type PendingDecision struct {
	Tool      string          `json:"tool"`
	CallID    string          `json:"callId"`
	SessionID string          `json:"sessionId"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// EffectiveTool is the tool the call reaches: the MCP tool a bridged call
// names in its arguments, or the call's own.
func (p PendingDecision) EffectiveTool() string {
	if p.Tool != bridgeToolName {
		return p.Tool
	}
	var call struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(p.Arguments, &call) != nil || call.Name == "" {
		return p.Tool
	}
	return call.Name
}
