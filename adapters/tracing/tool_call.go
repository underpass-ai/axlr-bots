package tracing

import (
	"encoding/json"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// toolCall is one tool call of an execution, paired from the
// tool.started and tool.ended items of its progress events.
type toolCall struct {
	tool    string
	mcpTool string
	callID  string
	summary string
	status  string
	err     string
	start   time.Time
	end     time.Time
	// open says no tool.ended came: the run ended before the call did,
	// or its end was never reported.
	open bool
}

// toolCallsOf pairs the progress items of the execution's events, in the
// order the calls started.
func toolCallsOf(events []domain.Event, execution domain.ExecutionID) []toolCall {
	var calls []toolCall
	byID := map[string]int{}
	for _, event := range events {
		if event.Type != domain.EventTaskProgress || event.ExecutionID != execution {
			continue
		}
		for _, item := range progressItems(event) {
			switch item.Kind {
			case application.ProgressToolStarted:
				call := toolCall{tool: item.Tool, mcpTool: item.MCPTool, callID: item.CallID, summary: item.Summary, start: item.At, open: true}
				if item.CallID != "" {
					byID[item.CallID] = len(calls)
				}
				calls = append(calls, call)
			case application.ProgressToolEnded:
				index, found := byID[item.CallID]
				if !found || item.CallID == "" {
					// An end whose start was lost: the duration it carries
					// places the start.
					calls = append(calls, toolCall{tool: item.Tool, callID: item.CallID, start: item.At.Add(-time.Duration(item.DurationMS) * time.Millisecond)})
					index = len(calls) - 1
				}
				call := &calls[index]
				call.open, call.end, call.status, call.err = false, item.At, item.Status, item.Error
			}
		}
	}
	return calls
}

// progressItems decodes the items of a progress event, whatever the
// event log made of the payload (typed in memory, generic after a trip
// through JSON).
func progressItems(event domain.Event) []application.ProgressItem {
	raw, err := json.Marshal(event.Payload["items"])
	if err != nil {
		return nil
	}
	var items []application.ProgressItem
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	return items
}
