package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// askTransport answers asks as its fields say.
type askTransport struct {
	fakeTransport
	answer application.SubscribeAnswer
	err    error
	asked  application.SubscribeCommand
}

func (a *askTransport) Subscribe(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, command application.SubscribeCommand) (application.SubscribeAnswer, error) {
	a.asked = command
	return a.answer, a.err
}

func TestTheSubscribeToolAsksTheOperator(t *testing.T) {
	tool := subscribeTool()
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || tool.Name != application.SubscribeToolName || strings.Join(schema.Required, ",") != "topic,why" || !strings.Contains(tool.Description, "until a person approves it") {
		t.Fatalf("tool = %+v %v", tool, err)
	}
	transport := &askTransport{}
	s := subscriber{transport: transport, node: "web-1", execution: "exec-1", log: log.New(io.Discard, "", 0)}
	if result := s.call(context.Background(), json.RawMessage(`{"topic": "error.*"}`)); !result.IsError {
		t.Error("an ask without why was taken")
	}
	transport.err = errors.New("down")
	if result := s.call(context.Background(), json.RawMessage(`{"topic": "error.*", "why": "retry"}`)); !result.IsError || !strings.Contains(result.Content[0].Text, "down") {
		t.Errorf("down = %+v", result)
	}
	transport.err = nil
	transport.answer = application.SubscribeAnswer{Topic: "error.*", Refused: &application.SubscribeRefusal{Reason: domain.RefusedAlreadySubscribed, Message: "already"}}
	if result := s.call(context.Background(), json.RawMessage(`{"topic": "error.*", "why": "retry"}`)); !result.IsError || !strings.Contains(result.Content[0].Text, domain.RefusedAlreadySubscribed) {
		t.Errorf("refused = %+v", result)
	}
	transport.answer = application.SubscribeAnswer{Topic: "error.*", Status: "pending"}
	if result := s.call(context.Background(), json.RawMessage(`{"topic": "error.*", "why": "retry"}`)); result.IsError || !strings.HasPrefix(result.Content[0].Text, "asked; your operator decides") || transport.asked.Why != "retry" {
		t.Errorf("asked = %+v %+v", result, transport.asked)
	}
}
