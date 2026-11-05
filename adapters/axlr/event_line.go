package axlr

import "encoding/json"

// eventLine is one line of `axlr-tui --exec --json`: an event of the turn,
// the receipt written just before the result, or the result that ends
// the execution.
type eventLine struct {
	Type     string `json:"type"`
	Session  string `json:"session,omitempty"`
	Text     string `json:"text,omitempty"`
	Tool     string `json:"tool,omitempty"`
	CallID   string `json:"call_id,omitempty"`
	Decision string `json:"decision,omitempty"`
	Status   string `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
	// Arguments are a call's arguments, on the tool.requested line of
	// a build that writes them; AXLR v0.14.0 writes none.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// PluginTool is the plugin's tool a call reaches, plugin/tool, as the
	// run resolved it, on the tool lines of a build that writes it.
	PluginTool string          `json:"plugin_tool,omitempty"`
	Result     *resultObject   `json:"result,omitempty"`
	Receipt    json.RawMessage `json:"receipt,omitempty"`
}
