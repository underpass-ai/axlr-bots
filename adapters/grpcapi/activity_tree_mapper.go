package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
)

// activityTree maps the tree to the wire, subtree usage included.
func activityTree(t application.TaskActivity) *operatorv1.TaskActivity {
	out := &operatorv1.TaskActivity{
		Task: activityTask(t.Task), Events: activityEvents(t.Events), SubtreeUsage: activityUsage(t.SubtreeUsage),
	}
	for _, e := range t.Executions {
		item := &operatorv1.ExecutionActivity{Execution: activityExecution(e.Execution), CompletedItems: activityItems(e.Completed)}
		if e.Cut != nil {
			item.ProgressCut = activityCut(*e.Cut)
		}
		out.Executions = append(out.Executions, item)
	}
	for _, c := range t.Children {
		out.Children = append(out.Children, activityTree(c))
	}
	return out
}
