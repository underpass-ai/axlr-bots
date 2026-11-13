package axlr

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// SessionItemReader reads the items of a run out of the file AXLR keeps
// for a session: the assistant's words and calls, and each tool's answer
// as the model saw it. It is the transcript's counterpart of the
// progress tracker, which reads the same run from its lines as it goes.
type SessionItemReader struct{}

var _ application.SessionItemReader = SessionItemReader{}

// sessionFile is the part of a session file the items come from.
type sessionFile struct {
	Messages []sessionMessage `json:"messages"`
	Activity []struct {
		Call struct {
			ID string `json:"id"`
		} `json:"call"`
		Decision string `json:"decision"`
	} `json:"activity"`
}

type sessionMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []sessionCall `json:"tool_calls"`
	ToolCallID string        `json:"tool_call_id"`
}

type sessionCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolEnvelope is what a tool answers as the model saw it, when it
// answers JSON.
type toolEnvelope struct {
	RequestID  string          `json:"request_id"`
	Status     string          `json:"status"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	DurationMS *int64          `json:"duration_ms"`
	Error      json.RawMessage `json:"error"`
	Output     json.RawMessage `json:"output"`
}

// ReadItems turns the session into items, oldest first. A session file
// keeps no clock of its own: a tool's answer carries the times it ran
// and everything else takes the time of the last item that had one.
func (SessionItemReader) ReadItems(content []byte) ([]application.ProgressItem, error) {
	var file sessionFile
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, err
	}
	decisions := map[string]string{}
	for _, a := range file.Activity {
		decisions[a.Call.ID] = a.Decision
	}
	var items []application.ProgressItem
	var now time.Time
	calls := map[string]sessionCall{}
	for _, m := range file.Messages {
		switch m.Role {
		case "assistant":
			if strings.TrimSpace(m.Content) != "" {
				items = append(items, application.ProgressItem{Kind: application.ProgressText, At: now, Text: m.Content})
			}
			for _, call := range m.ToolCalls {
				calls[call.ID] = call
				mcpTool, summary := describeArguments(call.Name, call.Arguments)
				items = append(items, application.ProgressItem{Kind: application.ProgressToolStarted, At: now, Tool: call.Name, CallID: call.ID, MCPTool: mcpTool, Summary: summary})
			}
		case "tool":
			id := m.ToolCallID
			var env toolEnvelope
			enveloped := json.Unmarshal([]byte(m.Content), &env) == nil
			if id == "" {
				id = env.RequestID
			}
			call := calls[id]
			item := application.ProgressItem{Kind: application.ProgressToolEnded, At: now, Tool: call.Name, CallID: id, Status: "ok"}
			if enveloped {
				if !env.FinishedAt.IsZero() {
					now = env.FinishedAt.UTC()
					item.At = now
				}
				if env.DurationMS != nil {
					item.DurationMS = *env.DurationMS
				}
				if failedStatus(env.Status) || hasError(env.Error) {
					item.Status = "failed"
					item.Error = firstLine(compactRunError(errorText(env.Error)))
				}
			}
			if decisions[id] == "deny" {
				item.Status = "denied"
			}
			items = append(items, item)
			if call.Name == application.DelegateToolName {
				items = append(items, delegationItem(call, item.At))
			}
		}
	}
	return items, nil
}

// failedStatus reports the statuses a tool answers when it did not do
// what it was asked.
func failedStatus(status string) bool {
	switch status {
	case "failed", "error", "timed_out", "rejected":
		return true
	}
	return false
}

func hasError(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != `""`
}

// errorText is an error as text, whether the tool wrote a string or an
// object.
func errorText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Message != "" {
		return object.Message
	}
	return string(raw)
}

// delegationItem is the item a call to the delegation tool tells: the
// bot it asked.
func delegationItem(call sessionCall, at time.Time) application.ProgressItem {
	var args struct {
		Bot string `json:"bot"`
	}
	_ = json.Unmarshal(call.Arguments, &args)
	return application.ProgressItem{Kind: application.ProgressDelegation, At: at, Bot: args.Bot}
}

// firstLine is the first line of a failure, cut like a progress item's
// error.
func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	runes := []rune(line)
	if len(runes) > 240 {
		return string(runes[:239]) + "…"
	}
	return line
}
