package nodeagent

import "encoding/json"

// mcpTool is a tool as tools/list describes it.
type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
