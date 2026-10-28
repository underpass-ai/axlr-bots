package nodeagent

import (
	"context"
	"encoding/json"
	"log"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// subscribeTool describes axlr_bots_subscribe: the bot asks its
// operator to let it hear a topic of its team.
func subscribeTool() mcpTool {
	properties := map[string]any{
		"topic": map[string]any{"type": "string", "minLength": 1, "maxLength": domain.MaxEventNameLength, "description": "a name or pattern, e.g. error.* or pedido.nuevo"},
		"why":   map[string]any{"type": "string", "minLength": 1, "maxLength": domain.MaxSubscriptionWhy},
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": []string{"topic", "why"}, "additionalProperties": false})
	return mcpTool{Name: application.SubscribeToolName, Description: "Ask your operator to let you hear a topic of your team. You hear nothing until a person approves it. Say in why what you would do with it.", InputSchema: schema}
}

// subscriber carries out one execution's calls of axlr_bots_subscribe.
type subscriber struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	log       *log.Logger
}

func (s subscriber) call(ctx context.Context, raw json.RawMessage) mcpToolResult {
	var command application.SubscribeCommand
	if err := json.Unmarshal(raw, &command); err != nil || command.Topic == "" || command.Why == "" {
		return textResult(`axlr_bots_subscribe takes {"topic": "<name or pattern>", "why": "<what you would do with it>"}`, true)
	}
	answer, err := s.transport.Subscribe(ctx, s.node, s.execution, command)
	if err != nil {
		s.log.Printf("node: execution %s: ask for %s failed: %v", s.execution, command.Topic, err)
		return textResult("the control plane did not take the ask: "+err.Error(), true)
	}
	if answer.Refused != nil {
		return jsonResult(map[string]string{"refused": answer.Refused.Reason, "message": answer.Refused.Message}, true)
	}
	s.log.Printf("node: execution %s: asked to hear %s", s.execution, answer.Topic)
	return textResult("asked; your operator decides. You hear "+answer.Topic+" once a person approves it.", false)
}
