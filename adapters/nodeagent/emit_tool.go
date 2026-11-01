package nodeagent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// emitTool describes axlr_bots_emit_event for one execution: the names
// it may use, the kinds and its teammates are in the description, the
// teammates in the schema. The name's kind fixes the severity, so the
// schema has none. The control plane checks every call again; the
// description only helps the model ask what will be taken.
func emitTool(offer application.EventsOffer) mcpTool {
	teammates := "none Enabled now"
	if len(offer.Teammates) > 0 {
		teammates = strings.Join(offer.Teammates, ", ")
	}
	description := fmt.Sprintf("Tell your team that something happened, at once and without waiting for anyone: a warning, a stop, a finding, a change of plan. The control plane gives the event to each teammate that hears it and records it for the people watching. The first segment of the name is its kind and says how much it matters: stop. and danger. are critical: the teammates that hear them stop what they do; warn. reaches a teammate while it works; info. is for the record and for whoever subscribed; error. and done. are the control plane's own. To stop a teammate, emit stop.halt to: its id. Use axlr_bots_delegate instead to ask a teammate for a task and its answer. Names you may use: %s. Your teammates: %s. Write the message as one line a person reads: what happened and why.", strings.Join(offer.Names, ", "), teammates)
	properties := map[string]any{
		"name":    map[string]any{"type": "string", "minLength": 1, "maxLength": domain.MaxEventNameLength, "description": "a dotted name whose first segment is its kind, e.g. stop.halt, warn.stock or info.deploy-seen"},
		"message": map[string]any{"type": "string", "minLength": 1, "maxLength": domain.MaxEventMessageLength, "description": "one line for people and bots: what happened and why"},
		"data":    map[string]any{"type": "object", "description": "facts for the receivers, at most 8 KiB as JSON"},
	}
	to := map[string]any{"type": "array", "maxItems": application.MaxEventTargets, "description": "only these teammates; absent: the whole team"}
	if len(offer.Teammates) > 0 {
		to["items"] = map[string]any{"type": "string", "enum": append([]string{}, offer.Teammates...)}
	} else {
		to["items"] = map[string]any{"type": "string"}
	}
	properties["to"] = to
	// Encoded, never concatenated: a name or an id is data, not JSON.
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": []string{"name", "message"}, "additionalProperties": false})
	return mcpTool{Name: application.EmitEventToolName, Description: description, InputSchema: schema}
}
