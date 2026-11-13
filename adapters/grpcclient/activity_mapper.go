package grpcclient

import (
	"strings"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/application/taskactivity"
)

func activityOptions(o taskactivity.Options) *operatorv1.ActivityOptions {
	return &operatorv1.ActivityOptions{
		ExcludeChildren: o.ExcludeChildren, MaxDepth: int32(o.MaxDepth),
		SkipCompletedProgress: o.SkipCompletedProgress, MaxCompletedItems: int32(o.MaxCompletedItems),
	}
}

func activityOf(a *operatorv1.TaskActivity) taskactivity.Activity {
	activity := taskactivity.Activity{
		Task: taskSnapshot(a.GetTask()), Events: eventsOf(a.GetEvents()), Subtree: usageOf(a.GetSubtreeUsage()),
		Executions: []taskactivity.Execution{}, Children: []taskactivity.Activity{},
	}
	for _, e := range a.GetExecutions() {
		activity.Executions = append(activity.Executions, taskactivity.Execution{
			Snapshot: executionSnapshot(e.GetExecution()), Cut: cutOf(e.GetProgressCut()), Completed: progressItemsOf(e.GetCompletedItems()),
		})
	}
	for _, child := range a.GetChildren() {
		activity.Children = append(activity.Children, activityOf(child))
	}
	return activity
}

func cutOf(c *operatorv1.ProgressCut) *taskactivity.Cut {
	if c == nil {
		return nil
	}
	return &taskactivity.Cut{
		Reason: enumWord(c.GetReason().String(), "PROGRESS_CUT_REASON_"), EventsKept: int(c.GetEventsKept()), BytesKept: c.GetBytesKept(),
		CutAt: timeOf(c.GetCutAt()), Dropped: int(c.GetDropped()), Completion: enumWord(c.GetCompletion().String(), "COMPLETION_SOURCE_"),
		TranscriptSessions: c.GetTranscriptSessions(), CompletionBounded: c.GetCompletionBounded(),
	}
}

// enumWord is an enum's name as the lowercase word the operator reads:
// PROGRESS_CUT_REASON_EVENT_LIMIT is event_limit, UNSPECIFIED is empty.
func enumWord(name, prefix string) string {
	word := strings.ToLower(strings.TrimPrefix(name, prefix))
	if word == "unspecified" {
		return ""
	}
	return word
}

func progressItemsOf(items []*operatorv1.ProgressItem) []application.ProgressItem {
	var out []application.ProgressItem
	for _, i := range items {
		out = append(out, application.ProgressItem{
			Kind: application.ProgressKind(i.GetKind()), At: timeOf(i.GetAt()), Tool: i.GetTool(), MCPTool: i.GetMcpTool(), CallID: i.GetCallId(),
			Summary: i.GetSummary(), Status: i.GetStatus(), Error: i.GetError(), DurationMS: i.GetDuration().AsDuration().Milliseconds(),
			Bot: i.GetBot(), TaskID: i.GetTaskId(), Reason: i.GetReason(), Text: i.GetText(),
		})
	}
	return out
}

// updateOf is one response of the live stream as the update it carries;
// a heartbeat, or a kind this client does not know, is the empty update.
func updateOf(r *operatorv1.WatchTaskActivityResponse) taskactivity.Update {
	switch u := r.GetUpdate().(type) {
	case *operatorv1.WatchTaskActivityResponse_Snapshot:
		snapshot := activityOf(u.Snapshot)
		return taskactivity.Update{Snapshot: &snapshot}
	case *operatorv1.WatchTaskActivityResponse_TaskChanged:
		task := taskSnapshot(u.TaskChanged.GetTask())
		return taskactivity.Update{TaskChanged: &task}
	case *operatorv1.WatchTaskActivityResponse_ExecutionChanged:
		execution := executionSnapshot(u.ExecutionChanged.GetExecution())
		return taskactivity.Update{ExecutionChanged: &execution}
	case *operatorv1.WatchTaskActivityResponse_EventAppended:
		return taskactivity.Update{EventAppended: &taskactivity.EventAppended{TaskID: u.EventAppended.GetTaskId(), Event: eventOf(u.EventAppended.GetEvent())}}
	case *operatorv1.WatchTaskActivityResponse_ChildAttached:
		return taskactivity.Update{ChildAttached: &taskactivity.ChildAttached{ParentTaskID: u.ChildAttached.GetParentTaskId(), Child: activityOf(u.ChildAttached.GetChild())}}
	case *operatorv1.WatchTaskActivityResponse_ProgressCompleted:
		return taskactivity.Update{ProgressCompleted: &taskactivity.ProgressCompleted{
			TaskID: u.ProgressCompleted.GetTaskId(), ExecutionID: u.ProgressCompleted.GetExecutionId(),
			Cut: cutOf(u.ProgressCompleted.GetProgressCut()), Items: progressItemsOf(u.ProgressCompleted.GetItems()),
		}}
	}
	return taskactivity.Update{}
}
