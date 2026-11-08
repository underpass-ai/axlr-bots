package domain

// bridgeToolName is the runtime's tool an MCP call is made through.
const bridgeToolName = "axlr_call_tool"

// writeDone reports a call that completed and was no preview: one that
// failed, was refused by its schema, was denied or is pending wrote
// nothing the ladder can count.
func (c ToolCall) writeDone() bool {
	return c.Status == "completed" && !c.Preview
}

// CountWrites is how many of a run's calls were completed writes. A run
// acting alone has one MCP tool on its allow list, so a bridged call can
// only be that write.
func CountWrites(calls []ToolCall, tool ToolName) int {
	writes := 0
	for _, call := range calls {
		if (call.Name == tool.String() || call.Name == bridgeToolName) && call.writeDone() {
			writes++
		}
	}
	return writes
}
