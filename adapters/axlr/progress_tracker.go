package axlr

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/underpass-ai/axlr-bots/application"
)

// Bounds of what one run tells as it goes: the model's words, its
// reasoning, and a call's summary of its arguments.
const (
	maxProgressText     = 32 << 10
	maxProgressThinking = 16 << 10
	maxProgressSummary  = 120
)

// callToolName is AXLR's bridge to a plugin's tool: its arguments name
// the MCP tool it calls.
const callToolName = "axlr_call_tool"

// progressTracker turns the lines of a run into progress items as the
// run writes them: a call asked for and how it ended, with how long it
// took on this host's clock, a call that waits for a person, and the
// model's words and its reasoning, each up to a cap, then one mark that
// they were cut. It hands each item over at once; whoever hears them
// must not block.
type progressTracker struct {
	tell     func(application.ProgressItem)
	now      func() time.Time
	started  map[string]time.Time
	words    cappedStream
	thinking cappedStream
}

func newProgressTracker(tell func(application.ProgressItem)) *progressTracker {
	return &progressTracker{
		tell: tell, now: time.Now, started: map[string]time.Time{},
		words:    cappedStream{kind: application.ProgressText, cutKind: application.ProgressTextTruncated, limit: maxProgressText},
		thinking: cappedStream{kind: application.ProgressThinking, cutKind: application.ProgressThinkingTruncated, limit: maxProgressThinking},
	}
}

// feed reads one line of the run.
func (t *progressTracker) feed(line []byte) {
	if t == nil || t.tell == nil {
		return
	}
	var event eventLine
	if json.Unmarshal(line, &event) != nil {
		return
	}
	now := t.now()
	switch event.Type {
	case "text":
		t.words.feed(event.Text, now, t.tell)
	case "reasoning":
		t.thinking.feed(event.Text, now, t.tell)
	case "tool.requested":
		t.started[event.CallID] = now
		mcpTool, summary := describeArguments(event.Tool, event.Arguments)
		if event.PluginTool != "" {
			// The run's own resolution names the tool behind an opaque
			// mcp_ alias; the alias in the arguments does not.
			mcpTool = event.PluginTool
		}
		t.tell(application.ProgressItem{Kind: application.ProgressToolStarted, At: now, Tool: event.Tool, CallID: event.CallID, MCPTool: mcpTool, Summary: summary})
	case "tool.completed":
		item := application.ProgressItem{Kind: application.ProgressToolEnded, At: now, Tool: event.Tool, CallID: event.CallID, Status: endedStatus(event), Error: compactRunError(event.Error)}
		if started, ok := t.started[event.CallID]; ok {
			item.DurationMS = now.Sub(started).Milliseconds()
			delete(t.started, event.CallID)
		}
		t.tell(item)
	case "decision.required":
		t.tell(application.ProgressItem{Kind: application.ProgressDecisionRequired, At: now, Tool: event.Tool, CallID: event.CallID})
	}
}

// endedStatus is how a call ended as its line says: denied by a person
// or the policy, failed, or ok. AXLR v0.14.0 writes only the decision;
// later builds write the status, ok or failed, and a failure's error.
func endedStatus(event eventLine) string {
	switch {
	case event.Decision == "deny", event.Status == "denied", event.Status == "forbidden":
		return "denied"
	case event.Error != "", event.Status == "failed", event.Status == "error":
		return "failed"
	}
	return "ok"
}

// describeArguments reads what a call's arguments say about it, when
// its line carries them: the MCP tool an axlr_call_tool names, and a
// short summary, never the whole arguments.
func describeArguments(tool string, raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}
	if tool == callToolName {
		var bridged struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(raw, &bridged) == nil && bridged.Name != "" {
			return bridged.Name, summarize(bridged.Arguments)
		}
	}
	return "", summarize(raw)
}

// summarize is the arguments as one line of JSON, cut short.
func summarize(raw json.RawMessage) string {
	compact := string(compactJSON(raw))
	if compact == "" || compact == "{}" || compact == "null" {
		return ""
	}
	compact = strings.ToValidUTF8(compact, "")
	if utf8.RuneCountInString(compact) <= maxProgressSummary {
		return compact
	}
	return string([]rune(compact)[:maxProgressSummary-1]) + "…"
}
