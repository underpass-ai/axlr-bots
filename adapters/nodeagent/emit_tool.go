package nodeagent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// emitTool describes axlr_bots_emit_event for one execution: the names
// it may use and its teammates are in the description, the severities
// up to its highest and the teammates in the schema. The control plane
// checks every call again; the description only helps the model ask
// what will be taken.
func emitTool(offer application.EventsOffer) mcpTool {
	severities := []string{}
	for _, s := range domain.Severity(offer.MaxSeverity).UpTo() {
		severities = append(severities, s.String())
	}
	teammates := "none Enabled now"
	if len(offer.Teammates) > 0 {
		teammates = strings.Join(offer.Teammates, ", ")
	}
	description := fmt.Sprintf("Tell your team that something happened, at once and without waiting for anyone: a warning, a recall, a sighting, a change of plan. The control plane gives a task to each teammate that hears it and records it for the people watching. Use it to warn or recall a teammate (to: its id, severity warning: it stops what it is doing and acts on your message); use axlr_bots_delegate instead to ask a teammate for a task and its answer. Names you may use: %s. Your teammates: %s. Severity: info for the record; warning tells the people watching and stops the teammates it is addressed to. Write the message as one line a person reads: what happened and why.", strings.Join(offer.Names, ", "), teammates)
	properties := map[string]any{
		"name":     map[string]any{"type": "string", "minLength": 1, "maxLength": domain.MaxEventNameLength, "description": "a short dotted name, e.g. recall or ship.sighted"},
		"message":  map[string]any{"type": "string", "minLength": 1, "maxLength": domain.MaxEventMessageLength, "description": "one line for people and bots: what happened and why"},
		"severity": map[string]any{"type": "string", "enum": severities, "default": "info"},
		"data":     map[string]any{"type": "object", "description": "facts for the receivers, at most 8 KiB as JSON"},
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
