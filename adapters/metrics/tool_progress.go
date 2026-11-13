package metrics

import (
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// endedToolCalls are the tool calls a bot.task.progress event reports
// as ended. The control plane publishes the items typed; one that went
// through JSON (a replayed log) is read the same way.
func endedToolCalls(event domain.Event) []application.ProgressItem {
	var items []application.ProgressItem
	switch raw := event.Payload["items"].(type) {
	case []application.ProgressItem:
		items = raw
	case nil:
		return nil
	default:
		encoded, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(encoded, &items) != nil {
			return nil
		}
	}
	var ended []application.ProgressItem
	for _, item := range items {
		if item.Kind == application.ProgressToolEnded {
			ended = append(ended, item)
		}
	}
	return ended
}

// toolStatus is the status label of an ended call: the three the
// runtime reports, and "other" for anything else, so the label's values
// stay a bounded set.
func toolStatus(status string) string {
	switch status {
	case "ok", "denied", "failed":
		return status
	}
	return "other"
}
