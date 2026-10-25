package nodeagent

// mcpContent is one block of a tool result; the server only writes text.
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
