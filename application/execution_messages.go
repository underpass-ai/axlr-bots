package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ExecutionMessages answers a node the operator's messages for an
// execution it runs, oldest first, after the one named by after (all of
// them when after is empty or no longer in the task's window). A node
// that does not hold the execution is refused; an execution that ended
// has none left to read.
type ExecutionMessages struct {
	deps Dependencies
}

// NewExecutionMessages wires the use case.
func NewExecutionMessages(deps Dependencies) ExecutionMessages { return ExecutionMessages{deps: deps} }

// Execute reads the messages.
func (uc ExecutionMessages) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID, after string) ([]RelayedMessage, error) {
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if execution.NodeID() != node {
		return nil, domain.StateError{Subject: "execution " + id.String(), Current: "assigned to " + orNobody(execution.NodeID()), Attempted: "read its messages for node " + node.String()}
	}
	messages := []RelayedMessage{}
	if execution.Outcome().IsFinal() {
		return messages, nil
	}
	events, err := uc.deps.EventReader.ListTaskEvents(ctx, execution.TaskID(), MaxEventLimit)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if event.Type != domain.EventTaskMessage || event.ExecutionID != id {
			continue
		}
		if event.ID.String() == after {
			messages = messages[:0]
			continue
		}
		text, _ := event.Payload["text"].(string)
		message := RelayedMessage{ID: event.ID.String(), Text: text, CreatedAt: event.CreatedAt}
		if name, ok := event.Payload["event"].(string); ok && name != "" {
			data, _ := event.Payload["data"].(map[string]any)
			message.Event = &RelayedEvent{Name: name, Data: data}
		}
		messages = append(messages, message)
	}
	return messages, nil
}
