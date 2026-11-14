package grpcclient

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application/taskactivity"
)

func fullActivity() *operatorv1.TaskActivity {
	child := &operatorv1.TaskActivity{Task: &operatorv1.Task{Id: "t-2", BotId: "bot-b", Status: "Completed"}, SubtreeUsage: &operatorv1.Usage{Requests: 1, CostUsd: 0.1}}
	return &operatorv1.TaskActivity{
		Task: fullTask("t-1"),
		Executions: []*operatorv1.ExecutionActivity{{
			Execution: fullExecution("e-1"),
			ProgressCut: &operatorv1.ProgressCut{
				Reason: operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_EVENT_LIMIT, EventsKept: 400, BytesKept: 4096, CutAt: ts(t1), Dropped: 3,
				Completion: operatorv1.CompletionSource_COMPLETION_SOURCE_TRANSCRIPT, TranscriptSessions: []string{"s1"}, CompletionBounded: true,
			},
			CompletedItems: []*operatorv1.ProgressItem{
				{Kind: "tool_started", At: ts(t1), Tool: "local_exec", McpTool: "mcp", CallId: "c9", Summary: "ls"},
				{Kind: "tool_ended", At: ts(t1), Tool: "local_exec", CallId: "c9", Status: "ok", Error: "e", Duration: durationpb.New(1500 * time.Millisecond), Bot: "b", TaskId: "t", Reason: "r", Text: "x"},
			},
		}},
		Events:       []*operatorv1.Event{fullEvent("ev-1", "bot.task.submitted")},
		Children:     []*operatorv1.TaskActivity{child},
		SubtreeUsage: &operatorv1.Usage{Requests: 4, CostUsd: 0.35, CostUnknown: true},
	}
}

func TestTheActivityTreeIsMapped(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("ActivityService", "GetTaskActivity"), &operatorv1.GetTaskActivityResponse{Activity: fullActivity()})
	activity, err := h.client.Get(t.Context(), "t-1", taskactivity.Options{ExcludeChildren: true, MaxDepth: 2, SkipCompletedProgress: true, MaxCompletedItems: 9})
	if err != nil {
		t.Fatal(err)
	}
	if activity.Task.ID != "t-1" || len(activity.Executions) != 1 || len(activity.Events) != 1 || len(activity.Children) != 1 || activity.Children[0].Task.ID != "t-2" {
		t.Fatalf("tree: %+v", activity)
	}
	if activity.Subtree.Requests != 4 || activity.Subtree.CostUSD != 0.35 || !activity.Subtree.CostUnknown {
		t.Errorf("subtree usage: %+v", activity.Subtree)
	}
	execution := activity.Executions[0]
	cut := execution.Cut
	if execution.Snapshot.ID != "e-1" || cut == nil || cut.Reason != "event_limit" || cut.Completion != "transcript" || cut.EventsKept != 400 || cut.BytesKept != 4096 || cut.Dropped != 3 ||
		cut.TranscriptSessions[0] != "s1" || !cut.CompletionBounded || !cut.CutAt.Equal(t1) {
		t.Errorf("cut: %+v", cut)
	}
	if len(execution.Completed) != 2 || execution.Completed[0].MCPTool != "mcp" || execution.Completed[1].DurationMS != 1500 || execution.Completed[1].Error != "e" || execution.Completed[1].Bot != "b" || execution.Completed[1].Text != "x" {
		t.Errorf("completed: %+v", execution.Completed)
	}
	request := h.fake.request(method("ActivityService", "GetTaskActivity")).(*operatorv1.GetTaskActivityRequest)
	if o := request.GetOptions(); request.GetTaskId() != "t-1" || !o.GetExcludeChildren() || o.GetMaxDepth() != 2 || !o.GetSkipCompletedProgress() || o.GetMaxCompletedItems() != 9 {
		t.Errorf("request: %v", request)
	}
	h.fake.answer(method("ActivityService", "GetTaskActivity"), &operatorv1.GetTaskActivityResponse{Activity: &operatorv1.TaskActivity{Task: &operatorv1.Task{Id: "t-3"}, Executions: []*operatorv1.ExecutionActivity{{Execution: &operatorv1.Execution{Id: "e"}, ProgressCut: &operatorv1.ProgressCut{}}}}})
	bare, _ := h.client.Get(t.Context(), "t-3", taskactivity.Options{})
	if bare.Executions[0].Cut == nil || bare.Executions[0].Cut.Reason != "" || bare.Executions[0].Cut.Completion != "" || bare.Children == nil {
		t.Errorf("an unspecified enum is empty: %+v", bare.Executions[0].Cut)
	}
}

func TestASnapshotOverFourMebibytesIsTaken(t *testing.T) {
	h := newHarness(t)
	big := fullActivity()
	big.Task.Objective = strings.Repeat("x", 5<<20)
	h.fake.answer(method("ActivityService", "GetTaskActivity"), &operatorv1.GetTaskActivityResponse{Activity: big})
	activity, err := h.client.Get(t.Context(), "t-1", taskactivity.Options{})
	if err != nil || len(activity.Task.Objective) != 5<<20 {
		t.Fatalf("a snapshot of 5 MiB: %d %v", len(activity.Task.Objective), err)
	}
}

func TestWatchingTheActivityDeliversEveryKindOfUpdateAndEndsOK(t *testing.T) {
	h := newHarness(t)
	snapshot := &operatorv1.TaskActivity{Task: &operatorv1.Task{Id: "t-1", Status: "Running"}}
	h.fake.watch = []*operatorv1.WatchTaskActivityResponse{
		{Cursor: "1", Update: &operatorv1.WatchTaskActivityResponse_Snapshot{Snapshot: snapshot}},
		{Cursor: "2", Update: &operatorv1.WatchTaskActivityResponse_Heartbeat{Heartbeat: &operatorv1.Heartbeat{}}},
		{Cursor: "3", Update: &operatorv1.WatchTaskActivityResponse_ExecutionChanged{ExecutionChanged: &operatorv1.ExecutionChanged{Execution: &operatorv1.Execution{Id: "e-1", TaskId: "t-1", Outcome: "Running"}}}},
		{Cursor: "4", Update: &operatorv1.WatchTaskActivityResponse_EventAppended{EventAppended: &operatorv1.EventAppended{TaskId: "t-1", Event: fullEvent("ev-2", "bot.task.assigned")}}},
		{Cursor: "5", Update: &operatorv1.WatchTaskActivityResponse_ChildAttached{ChildAttached: &operatorv1.ChildAttached{ParentTaskId: "t-1", Child: &operatorv1.TaskActivity{Task: &operatorv1.Task{Id: "t-2", Status: "Pending"}}}}},
		{Cursor: "6", Update: &operatorv1.WatchTaskActivityResponse_ProgressCompleted{ProgressCompleted: &operatorv1.ProgressCompleted{
			TaskId: "t-1", ExecutionId: "e-1", ProgressCut: &operatorv1.ProgressCut{Reason: operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_BYTE_LIMIT},
			Items: []*operatorv1.ProgressItem{{Kind: "tool_started", Tool: "x", CallId: "c"}},
		}}},
		{Cursor: "7", Update: &operatorv1.WatchTaskActivityResponse_TaskChanged{TaskChanged: &operatorv1.TaskChanged{Task: &operatorv1.Task{Id: "t-1", Status: "Completed"}}}},
	}
	var tree taskactivity.Activity
	var cursors []string
	heartbeats := 0
	err := h.client.Watch(t.Context(), "t-1", taskactivity.Options{MaxDepth: 1}, "0", func(cursor string, update taskactivity.Update) error {
		cursors = append(cursors, cursor)
		if update.Heartbeat() {
			heartbeats++
		}
		tree.Apply(update)
		return nil
	})
	if err != nil {
		t.Fatalf("a stream the server ends with OK ends with no error: %v", err)
	}
	if strings.Join(cursors, "") != "1234567" || heartbeats != 1 {
		t.Errorf("cursors %v, heartbeats %d", cursors, heartbeats)
	}
	if tree.Task.Status != "Completed" || len(tree.Children) != 1 || len(tree.Events) != 1 || len(tree.Executions) != 1 ||
		tree.Executions[0].Cut == nil || tree.Executions[0].Cut.Reason != "byte_limit" || len(tree.Executions[0].Completed) != 1 {
		t.Errorf("tree after the stream: %+v", tree)
	}
	request := h.fake.request("WatchTaskActivity").(*operatorv1.WatchTaskActivityRequest)
	if request.GetTaskId() != "t-1" || request.GetResumeCursor() != "0" || request.GetOptions().GetMaxDepth() != 1 {
		t.Errorf("request: %v", request)
	}
	if got := h.fake.headers("WatchTaskActivity").Get("authorization"); len(got) != 1 || got[0] != "Bearer "+testToken {
		t.Errorf("the stream carries the token: %v", got)
	}
}

func TestWatchingStopsWhenTheCallbackSaysSoOrTheStreamFails(t *testing.T) {
	h := newHarness(t)
	h.fake.watch = []*operatorv1.WatchTaskActivityResponse{{Cursor: "1", Update: &operatorv1.WatchTaskActivityResponse_Heartbeat{Heartbeat: &operatorv1.Heartbeat{}}}}
	stop := status.Error(codes.Canceled, "stop")
	if err := h.client.Watch(t.Context(), "t", taskactivity.Options{}, "", func(string, taskactivity.Update) error { return stop }); err != stop {
		t.Errorf("the callback's error comes back as it is: %v", err)
	}
	h.fake.watchError = detailed(t, codes.NotFound, &operatorv1.ErrorDetail{Kind: "not_found", Message: "x", Resource: "task", Id: "t"})
	err := h.client.Watch(t.Context(), "t", taskactivity.Options{}, "", func(string, taskactivity.Update) error { return nil })
	if err == nil || err.Error() != "task t not found" {
		t.Errorf("a failure of the stream is mapped: %v", err)
	}
}

func TestApplyLeavesOutWhatTheTreeDoesNotHold(t *testing.T) {
	var tree taskactivity.Activity
	tree.Task.ID = "t-1"
	missing := "t-9"
	tree.Apply(taskactivity.Update{ChildAttached: &taskactivity.ChildAttached{ParentTaskID: missing}})
	tree.Apply(taskactivity.Update{EventAppended: &taskactivity.EventAppended{TaskID: missing}})
	tree.Apply(taskactivity.Update{ProgressCompleted: &taskactivity.ProgressCompleted{TaskID: "t-1", ExecutionID: "nope"}})
	tree.Apply(taskactivity.Update{})
	if len(tree.Children) != 0 || len(tree.Events) != 0 || len(tree.Executions) != 0 {
		t.Errorf("tree: %+v", tree)
	}
	child := taskactivity.Activity{}
	child.Task.ID = "t-2"
	tree.Apply(taskactivity.Update{ChildAttached: &taskactivity.ChildAttached{ParentTaskID: "t-1", Child: child}})
	tree.Apply(taskactivity.Update{ChildAttached: &taskactivity.ChildAttached{ParentTaskID: "t-1", Child: child}})
	if len(tree.Children) != 1 {
		t.Errorf("a child attached twice is one: %+v", tree.Children)
	}
}
