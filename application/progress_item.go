package application

import (
	"strings"
	"time"
)

// ProgressItem is one thing a running execution did, as its node read
// it from the run's lines. Which fields an item carries depends on its
// kind; the control plane keeps only those, cut to their bounds, so a
// tool's arguments never reach the timeline beyond a short summary.
type ProgressItem struct {
	Kind ProgressKind `json:"kind"`
	At   time.Time    `json:"at"`
	// Tool is the runtime's tool name; MCPTool the MCP tool an
	// axlr_call_tool names, when the line carries its arguments.
	Tool    string `json:"tool,omitempty"`
	MCPTool string `json:"mcpTool,omitempty"`
	CallID  string `json:"callId,omitempty"`
	// Summary is a short, cut summary of a call's arguments.
	Summary string `json:"summary,omitempty"`
	// Status is how a call ended: ok, denied or failed; Error the
	// first line of what a failed call answered.
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
	// Bot is the bot a delegation asked; TaskID the child it created;
	// Reason why the control plane refused it.
	Bot    string `json:"bot,omitempty"`
	TaskID string `json:"taskId,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Text is the model's words, or its reasoning for a thinking item.
	Text string `json:"text,omitempty"`
}

// Bounds of one progress item.
const (
	maxProgressName    = 200
	maxProgressSummary = 160
	maxProgressStatus  = 32
	maxProgressError   = 240
	// MaxProgressText bounds the words of one text or thinking item.
	MaxProgressText = 4000
)

// normalized keeps the fields of the item's kind, cut to their bounds;
// false for an item of no known kind or with nothing to say.
func (i ProgressItem) normalized(now time.Time) (ProgressItem, bool) {
	if !i.Kind.known() {
		return ProgressItem{}, false
	}
	at := i.At
	if at.IsZero() {
		at = now
	}
	out := ProgressItem{Kind: i.Kind, At: at.UTC()}
	call := func() {
		out.Tool = cut(i.Tool, maxProgressName)
		out.CallID = cut(i.CallID, maxProgressName)
	}
	switch i.Kind {
	case ProgressToolStarted:
		call()
		out.MCPTool = cut(i.MCPTool, maxProgressName)
		out.Summary = cut(i.Summary, maxProgressSummary)
		return out, out.Tool != ""
	case ProgressToolEnded:
		call()
		out.Status = cut(i.Status, maxProgressStatus)
		out.Error = cut(i.Error, maxProgressError)
		out.DurationMS = max(i.DurationMS, 0)
		return out, out.Tool != "" || out.CallID != ""
	case ProgressDecisionRequired:
		call()
		return out, out.Tool != ""
	case ProgressDelegation:
		out.Bot = cut(i.Bot, maxProgressName)
		out.TaskID = cut(i.TaskID, maxProgressName)
		out.Reason = cut(i.Reason, maxProgressName)
		return out, out.Bot != ""
	case ProgressText, ProgressThinking:
		out.Text = cut(i.Text, MaxProgressText)
		return out, strings.TrimSpace(out.Text) != ""
	}
	return out, true
}

// cut keeps at most limit characters of s, the last one an ellipsis
// when it was longer.
func cut(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
}
