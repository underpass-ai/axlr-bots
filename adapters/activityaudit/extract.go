package activityaudit

import (
	"encoding/json"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The statuses a call ended with that are failures: a call the policy or
// a person denied is a decision, not one.
const (
	statusFailed = "failed"
	statusOK     = "ok"
)

// call is one tool call of a task, as its timeline and its executions
// tell it, in the order it was asked for.
type call struct {
	order   int
	tool    string
	mcpTool string
	summary string
	status  string
	error   string
	at      time.Time
	seen    bool
}

// Extract reads a finished task's failures from what the operator API
// answers: the calls its executions' results list and the progress
// items of its timeline, joined by call id; and its failure reason when
// it ended Failed. team is the bot's team, empty when it is in none.
func Extract(task domain.TaskSnapshot, executions []domain.ExecutionSnapshot, timeline []domain.Event, team string) []Failure {
	calls, order := callsOf(executions, timeline)
	var failures []Failure
	for _, id := range order {
		c := calls[id]
		if c.status != statusFailed {
			continue
		}
		f := callFailure(c)
		f.Bot, f.Team, f.Task = task.BotID, team, task.ID
		if f.At.IsZero() {
			f.At = task.UpdatedAt
		}
		f.Recovered = recovered(c, f, calls, order)
		failures = append(failures, f)
	}
	if task.Status == domain.TaskFailed.String() {
		reason := task.FailureReason
		for i := len(executions) - 1; reason == "" && i >= 0; i-- {
			reason = executions[i].FailureReason
		}
		if reason == "" {
			reason = "no reason given"
		}
		failures = append(failures, Failure{Kind: KindTaskFailed, Bot: task.BotID, Team: team, Task: task.ID, Error: Normalise(reason), At: task.UpdatedAt})
	}
	return failures
}

// callsOf joins the executions' calls and the timeline's items by call
// id, in the order the timeline asked for them, then the ones only an
// execution lists.
func callsOf(executions []domain.ExecutionSnapshot, timeline []domain.Event) (map[string]*call, []string) {
	calls := map[string]*call{}
	var order []string
	get := func(id string) *call {
		if c, ok := calls[id]; ok {
			return c
		}
		c := &call{order: len(order)}
		calls[id] = c
		order = append(order, id)
		return c
	}
	for _, item := range progressItems(timeline) {
		if item.CallID == "" {
			continue
		}
		switch item.Kind {
		case application.ProgressToolStarted:
			c := get(item.CallID)
			c.tool, c.mcpTool, c.summary = item.Tool, item.MCPTool, item.Summary
		case application.ProgressToolEnded:
			c := get(item.CallID)
			if c.tool == "" {
				c.tool = item.Tool
			}
			c.status, c.error, c.at, c.seen = item.Status, item.Error, item.At, true
		}
	}
	for _, execution := range executions {
		for _, listed := range execution.Result.ToolCalls {
			if listed.CallID == "" {
				continue
			}
			c := get(listed.CallID)
			if c.tool == "" {
				c.tool = listed.Name
			}
			switch {
			case c.seen:
			case listed.Status == statusFailed:
				c.status = statusFailed
			case listed.Status == "completed":
				c.status = statusOK
			}
			if c.at.IsZero() && execution.EndedAt != nil {
				c.at = *execution.EndedAt
			}
		}
	}
	return calls, order
}

// progressItems are the items of the timeline's progress events, in
// order.
func progressItems(timeline []domain.Event) []application.ProgressItem {
	var items []application.ProgressItem
	for _, event := range timeline {
		if event.Type != domain.EventTaskProgress {
			continue
		}
		raw, err := json.Marshal(event.Payload["items"])
		if err != nil {
			continue
		}
		var batch []application.ProgressItem
		if json.Unmarshal(raw, &batch) == nil {
			items = append(items, batch...)
		}
	}
	return items
}

// callFailure is the failure of one failed call, before its bot and task.
func callFailure(c *call) Failure {
	f := Failure{Kind: KindToolCall, Tool: c.tool, At: c.at}
	if f.Tool == "" {
		f.Tool = unknownValue
	}
	read := readCallError(c.error)
	f.Error = Normalise(read.Text)
	if read.Cut || f.Error == "" {
		f.Error = cutMarker
	}
	switch f.Tool {
	case toolLocalExec:
		summary := readCallSummary(c.summary)
		f.Program, f.Arg = summary.Program, unknownValue
		if summary.ArgKnown {
			f.Arg = Normalise(summary.Arg)
		}
		f.Exit = read.Exit
		if f.Exit == "" {
			f.Exit = unknownValue
			if read.Cut && enveloped(c.error) {
				f.Exit = exitNonZero
			}
		}
		f.Partial = f.Program == ""
	case toolCallTool:
		f.MCPTool = c.mcpTool
		f.Partial = f.MCPTool == ""
	default:
		f.Partial = f.Error == cutMarker
	}
	return f
}

// enveloped reports whether the error is AXLR's response envelope of a
// run that completed: the program ran and its exit was not 0.
func enveloped(raw string) bool {
	return envelope.MatchString(raw) && completedRun.MatchString(raw)
}

// recovered reports whether a call of the same tool, the same program
// or plugin's tool, ended ok after the failed one: the model fixed its
// call and went on. A call whose line hid the program is never matched.
func recovered(failed *call, f Failure, calls map[string]*call, order []string) bool {
	if f.Partial {
		return false
	}
	for _, id := range order[failed.order+1:] {
		later := calls[id]
		if later.status != statusOK || later.tool != failed.tool {
			continue
		}
		switch f.Tool {
		case toolLocalExec:
			if readCallSummary(later.summary).Program == f.Program {
				return true
			}
		case toolCallTool:
			if later.mcpTool == f.MCPTool {
				return true
			}
		default:
			return true
		}
	}
	return false
}
