package kmpchannel

import "encoding/json"

// toolResult is what a tools/call answers: its text, and KMP's
// structured answer (the wake, or the error's code).
type toolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}
