package jev

import "context"

// toolClient is how a run reaches the tools of its program's server: over
// MCP's Streamable HTTP for a server of the node's mcp.json (mcpClient),
// or in process for the reserved server axlr-bots (nodeToolClient). The
// run's receipts and traces are the same either way.
type toolClient interface {
	initialize(ctx context.Context, clientName, version string) error
	callTool(ctx context.Context, name string, arguments map[string]any) (ToolResult, error)
	// address names where the tools are served, for the trace.
	address() string
}
