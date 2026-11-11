package jev

import (
	"context"
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// nodeToolClient reaches the tools the node serves in process, with the
// execution's identity: the reserved server axlr-bots of a team's Jev
// watcher (docs/architecture/team-channel.md, A6). No HTTP, no mcp.json:
// the node carries each call to the control plane over its own channel,
// as it does a tell.
type nodeToolClient struct {
	call func(ctx context.Context, tool string, arguments map[string]any) (application.NodeToolAnswer, error)
}

var _ toolClient = nodeToolClient{}

// initialize has nothing to open: the node is there.
func (c nodeToolClient) initialize(context.Context, string, string) error { return nil }

// callTool calls one tool; a refusal is the tool's error, as a server's
// isError is.
func (c nodeToolClient) callTool(ctx context.Context, name string, arguments map[string]any) (ToolResult, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	answer, err := c.call(ctx, name, arguments)
	if err != nil {
		return ToolResult{}, err
	}
	if answer.Refused != "" {
		return ToolResult{}, ToolError{Tool: name, Text: answer.Refused}
	}
	raw, err := json.Marshal(answer.Object)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Text: string(raw), Object: answer.Object}, nil
}

func (c nodeToolClient) address() string { return "node:" + domain.WatchServer }
