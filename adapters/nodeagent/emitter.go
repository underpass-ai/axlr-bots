package nodeagent

import (
	"context"
	"encoding/json"
	"log"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// emitter carries out one execution's calls of axlr_bots_emit_event:
// it hands the event to the control plane over the node's channel and
// answers at once with who got a task.
type emitter struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	log       *log.Logger
}

// emittedView is what the model reads about its event.
type emittedView struct {
	Event      string                      `json:"event"`
	Name       string                      `json:"name"`
	Severity   string                      `json:"severity"`
	Deliveries []application.EventDelivery `json:"tasks"`
	Skipped    []application.EventSkip     `json:"skipped,omitempty"`
	Note       string                      `json:"note,omitempty"`
}

func (e emitter) call(ctx context.Context, raw json.RawMessage) mcpToolResult {
	var command application.EmitEventCommand
	if err := json.Unmarshal(raw, &command); err != nil || command.Name == "" || command.Message == "" {
		return textResult("axlr_bots_emit_event takes {\"name\": \"<event>\", \"message\": \"<one line>\", \"severity\": \"info|warning\", \"to\": [\"<teammate>\"], \"data\": {}}", true)
	}
	answer, err := e.transport.Emit(ctx, e.node, e.execution, command)
	if err != nil {
		e.log.Printf("node: execution %s: event %s failed: %v", e.execution, command.Name, err)
		return textResult("the control plane did not take the event: "+err.Error(), true)
	}
	if answer.Refused != nil {
		e.log.Printf("node: execution %s: event %s refused (%s)", e.execution, command.Name, answer.Refused.Reason)
		return jsonResult(map[string]string{"refused": answer.Refused.Reason, "message": answer.Refused.Message}, true)
	}
	e.log.Printf("node: execution %s: emitted %s (%s) to %d teammates", e.execution, answer.EventID, answer.Name, len(answer.Deliveries))
	view := emittedView{Event: answer.EventID, Name: answer.Name, Severity: answer.Severity.String(), Deliveries: answer.Deliveries, Skipped: answer.Skipped}
	if view.Deliveries == nil {
		view.Deliveries = []application.EventDelivery{}
		view.Note = "no teammate took it as a task; it is recorded for the people watching"
	}
	return jsonResult(view, false)
}
