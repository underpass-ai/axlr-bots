package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/application/taskactivity"
	"github.com/underpass-ai/axlr-bots/domain"
)

// activityRenderer writes a task's activity tree as text people read: the
// task, its executions with duration and cost, the tools each ran with
// their state and duration, where the recorded progress was cut and what
// was completed from the transcript, the timeline, and the tasks it
// delegated, indented under it.
type activityRenderer struct {
	out io.Writer
}

func (r activityRenderer) render(activity taskactivity.Activity) {
	r.task(activity, 0)
	if len(activity.Children) > 0 || activity.Subtree.Requests > 0 || activity.Subtree.CostUSD > 0 {
		fmt.Fprintf(r.out, "\nin all: %s, %d model request(s), %d tool call(s)\n", cost(activity.Subtree), activity.Subtree.Requests, activity.Subtree.ToolCalls)
	}
}

func (r activityRenderer) task(activity taskactivity.Activity, depth int) {
	pad := strings.Repeat("  ", depth)
	task := activity.Task
	fmt.Fprintf(r.out, "%sTask %s  bot %s  %s%s\n", pad, task.ID, task.BotID, task.Status, trigger(task))
	fmt.Fprintf(r.out, "%s  objective: %s\n", pad, oneLine(task.Objective, 200))
	if task.FailureReason != "" {
		fmt.Fprintf(r.out, "%s  failed: %s\n", pad, oneLine(task.FailureReason, 300))
	}
	if task.Waiting != "" {
		fmt.Fprintf(r.out, "%s  waiting: %s\n", pad, task.Waiting)
	}
	if task.Approval != nil && task.Approval.Outcome == "" {
		fmt.Fprintf(r.out, "%s  waits for a decision on %s since %s (deadline %s)\n", pad, task.Approval.Tool, stamp(task.Approval.Since), stamp(task.Approval.Deadline))
	}
	progress := progressByExecution(activity.Events)
	for _, execution := range activity.Executions {
		r.execution(execution, progress[execution.Snapshot.ID], pad+"  ")
	}
	r.timeline(activity.Events, pad+"  ")
	for _, child := range activity.Children {
		fmt.Fprintln(r.out)
		r.task(child, depth+1)
	}
}

func trigger(task domain.TaskSnapshot) string {
	if task.Delegation != nil {
		return "  delegated by " + task.Delegation.ParentBotID + " (task " + task.Delegation.ParentTaskID + ")"
	}
	if task.Trigger.Type != "" && task.Trigger.Type != "manual" {
		return "  from " + task.Trigger.Type + " " + task.Trigger.Name
	}
	return ""
}

func (r activityRenderer) execution(execution taskactivity.Execution, recorded []application.ProgressItem, pad string) {
	e := execution.Snapshot
	where := ""
	if e.NodeID != "" {
		where = "  node " + e.NodeID
	}
	fmt.Fprintf(r.out, "%sExecution %s  %s%s  %s  %s\n", pad, e.ID, e.Outcome, where, elapsed(e), cost(e.Usage))
	fmt.Fprintf(r.out, "%s  %d model request(s), %d tool call(s), %d prompt / %d completion tokens\n", pad, e.Usage.Requests, e.Usage.ToolCalls, e.Usage.PromptTokens, e.Usage.CompletionTokens)
	if e.FailureReason != "" {
		fmt.Fprintf(r.out, "%s  failed: %s\n", pad, oneLine(e.FailureReason, 300))
	}
	for _, tool := range tools(recorded, "") {
		r.tool(tool, pad+"  ")
	}
	if cut := execution.Cut; cut != nil {
		fmt.Fprintf(r.out, "%s  -- progress timeline cut (%s) at %s: %d event(s), %d byte(s) kept", pad, cut.Reason, stamp(cut.CutAt), cut.EventsKept, cut.BytesKept)
		if cut.Dropped > 0 {
			fmt.Fprintf(r.out, ", %d item(s) dropped by the node", cut.Dropped)
		}
		fmt.Fprintln(r.out)
		if cut.Completion == "transcript" {
			fmt.Fprintf(r.out, "%s  -- completed from the transcript (%s)%s:\n", pad, strings.Join(cut.TranscriptSessions, ", "), bounded(cut))
		}
	}
	for _, tool := range tools(execution.Completed, "from transcript") {
		r.tool(tool, pad+"  ")
	}
}

func bounded(cut *taskactivity.Cut) string {
	if cut.CompletionBounded {
		return " (bounded: later items are not shown)"
	}
	return ""
}

// step is one thing a run did: a tool with its state, or a decision or a
// delegation.
type step struct {
	label    string
	status   string
	duration time.Duration
	detail   string
	source   string
}

func (r activityRenderer) tool(s step, pad string) {
	line := fmt.Sprintf("%s%-8s %s", pad, s.status, s.label)
	if s.duration > 0 {
		line += "  " + s.duration.Round(time.Millisecond).String()
	}
	if s.detail != "" {
		line += "  " + s.detail
	}
	if s.source != "" {
		line += "  [" + s.source + "]"
	}
	fmt.Fprintln(r.out, line)
}

// kind is a progress kind in the spelling of the node protocol,
// tool.started: the API also writes it tool_started.
func kind(k application.ProgressKind) string {
	return strings.ReplaceAll(string(k), "_", ".")
}

// tools pairs each tool's start with its end by call id and lists the
// tools, the decisions asked and the delegations in the order they began.
func tools(items []application.ProgressItem, source string) []step {
	var steps []step
	byCall := map[string]int{}
	for _, item := range items {
		switch kind(item.Kind) {
		case "tool.started":
			label := item.Tool
			if item.MCPTool != "" {
				label += " (" + item.MCPTool + ")"
			}
			byCall[item.CallID] = len(steps)
			steps = append(steps, step{label: label, status: "running", detail: oneLine(item.Summary, 120), source: source})
		case "tool.ended":
			index, found := byCall[item.CallID]
			if !found {
				steps = append(steps, step{label: item.Tool, source: source})
				index = len(steps) - 1
				byCall[item.CallID] = index
			}
			steps[index].status = endedStatus(item)
			steps[index].duration = time.Duration(item.DurationMS) * time.Millisecond
			if item.Error != "" {
				steps[index].detail = oneLine(item.Error, 160)
			}
		case "decision.required":
			steps = append(steps, step{label: item.Tool, status: "approval", detail: "decision required", source: source})
		case "delegation":
			steps = append(steps, step{label: item.Bot, status: "delegated", detail: "task " + item.TaskID, source: source})
		}
	}
	return steps
}

func endedStatus(item application.ProgressItem) string {
	switch {
	case item.Error != "":
		return "FAILED"
	case item.Status == "":
		return "ended"
	}
	return item.Status
}

// progressByExecution reads the items out of the task's own
// bot.task.progress events, by execution.
func progressByExecution(events []domain.Event) map[string][]application.ProgressItem {
	progress := map[string][]application.ProgressItem{}
	for _, event := range events {
		if event.Type != domain.EventTaskProgress {
			continue
		}
		raw, err := json.Marshal(event.Payload["items"])
		if err != nil {
			continue
		}
		var items []application.ProgressItem
		if json.Unmarshal(raw, &items) == nil {
			progress[event.ExecutionID.String()] = append(progress[event.ExecutionID.String()], items...)
		}
	}
	return progress
}

// timeline lists the task's own events, leaving the progress ones out:
// they are the tools above.
func (r activityRenderer) timeline(events []domain.Event, pad string) {
	var lines []string
	for _, event := range events {
		if event.Type != domain.EventTaskProgress {
			lines = append(lines, fmt.Sprintf("%s  %s  %s", pad, stamp(event.CreatedAt), event.Type))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(r.out, "%stimeline:\n%s\n", pad, strings.Join(lines, "\n"))
}

func elapsed(e domain.ExecutionSnapshot) string {
	if e.EndedAt == nil {
		return "running since " + stamp(e.StartedAt)
	}
	return e.EndedAt.Sub(e.StartedAt).Round(time.Millisecond).String()
}

// cost is the cost in dollars, or that it is not known: unknown is not
// free.
func cost(u domain.Usage) string {
	switch {
	case u.CostUnknown && u.CostUSD > 0:
		return fmt.Sprintf("$%.4f and more (some cost unknown)", u.CostUSD)
	case u.CostUnknown:
		return "cost unknown"
	}
	return fmt.Sprintf("$%.4f", u.CostUSD)
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04:05Z")
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}
