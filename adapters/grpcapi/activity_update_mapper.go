package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
)

// activityResponse maps one update of a watch to the wire.
func activityResponse(u application.ActivityUpdate) *operatorv1.WatchTaskActivityResponse {
	out := &operatorv1.WatchTaskActivityResponse{Cursor: u.Cursor}
	switch {
	case u.Snapshot != nil:
		out.Update = &operatorv1.WatchTaskActivityResponse_Snapshot{Snapshot: activityTree(*u.Snapshot)}
	case u.Task != nil:
		out.Update = &operatorv1.WatchTaskActivityResponse_TaskChanged{TaskChanged: &operatorv1.TaskChanged{Task: taskToProto(u.Task.Snapshot())}}
	case u.Execution != nil:
		out.Update = &operatorv1.WatchTaskActivityResponse_ExecutionChanged{ExecutionChanged: &operatorv1.ExecutionChanged{Execution: executionToProto(u.Execution.Snapshot())}}
	case u.Event != nil:
		out.Update = &operatorv1.WatchTaskActivityResponse_EventAppended{EventAppended: &operatorv1.EventAppended{TaskId: u.Event.TaskID.String(), Event: eventToProto(u.Event.Event)}}
	case u.Child != nil:
		out.Update = &operatorv1.WatchTaskActivityResponse_ChildAttached{ChildAttached: &operatorv1.ChildAttached{ParentTaskId: u.Child.ParentTaskID.String(), Child: activityTree(u.Child.Child)}}
	case u.Completed != nil:
		out.Update = &operatorv1.WatchTaskActivityResponse_ProgressCompleted{ProgressCompleted: &operatorv1.ProgressCompleted{
			TaskId: u.Completed.TaskID.String(), ExecutionId: u.Completed.ExecutionID.String(),
			ProgressCut: activityCut(u.Completed.Cut), Items: activityItems(u.Completed.Items),
		}}
	default:
		out.Update = &operatorv1.WatchTaskActivityResponse_Heartbeat{Heartbeat: &operatorv1.Heartbeat{}}
	}
	return out
}
