package domain

// bridgeToolName is the runtime's tool an MCP call is made through.
const bridgeToolName = "axlr_call_tool"

// writeRan reports a call that reached its tool: one the runtime denied,
// forbade or kept pending did not.
func (c ToolCall) writeRan() bool {
	return c.Status == "completed" || c.Status == "failed" || c.Status == "uncertain"
}

// CountWrites is how many of a run's calls were its write. A run acting
// alone has one MCP tool on its allow list, so a bridged call can only be
// that write.
func CountWrites(calls []ToolCall, tool ToolName) int {
	writes := 0
	for _, call := range calls {
		if (call.Name == tool.String() || call.Name == bridgeToolName) && call.writeRan() {
			writes++
		}
	}
	return writes
}
