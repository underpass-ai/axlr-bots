package nodeagent

import (
	"context"
	"encoding/json"
)

// mcpHandler is one tool the node serves an execution: how tools/list
// describes it and what a tools/call of it does.
type mcpHandler struct {
	tool mcpTool
	call func(ctx context.Context, arguments json.RawMessage) mcpToolResult
}
