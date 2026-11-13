package taskactivity

import (
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestUpdatesChangeTheTreeWhereTheyBelong(t *testing.T) {
	tree := Activity{Task: domain.TaskSnapshot{ID: "t-1", Status: "Running"}}
	child := Activity{Task: domain.TaskSnapshot{ID: "t-2", Status: "Pending"}}
	tree.Apply(Update{ChildAttached: &ChildAttached{ParentTaskID: "t-1", Child: child}})
	tree.Apply(Update{TaskChanged: &domain.TaskSnapshot{ID: "t-2", Status: "Completed"}})
	tree.Apply(Update{ExecutionChanged: &domain.ExecutionSnapshot{ID: "e-1", TaskID: "t-2", Outcome: "Running"}})
	tree.Apply(Update{ExecutionChanged: &domain.ExecutionSnapshot{ID: "e-1", TaskID: "t-2", Outcome: "Completed"}})
	tree.Apply(Update{EventAppended: &EventAppended{TaskID: "t-2", Event: domain.Event{ID: "ev"}}})
	tree.Apply(Update{ProgressCompleted: &ProgressCompleted{TaskID: "t-2", ExecutionID: "e-1", Cut: &Cut{Reason: "event_limit"}, Items: []application.ProgressItem{{Kind: application.ProgressToolStarted}}}})
	got := tree.Children[0]
	if got.Task.Status != "Completed" || len(got.Executions) != 1 || got.Executions[0].Snapshot.Outcome != "Completed" || len(got.Events) != 1 ||
		got.Executions[0].Cut.Reason != "event_limit" || len(got.Executions[0].Completed) != 1 {
		t.Errorf("child: %+v", got)
	}
	tree.Apply(Update{Snapshot: &Activity{Task: domain.TaskSnapshot{ID: "t-9"}}})
	if tree.Task.ID != "t-9" || len(tree.Children) != 0 {
		t.Errorf("a snapshot replaces the tree: %+v", tree)
	}
	if !(Update{}).Heartbeat() || (Update{TaskChanged: &domain.TaskSnapshot{}}).Heartbeat() {
		t.Error("only the empty update is a heartbeat")
	}
}
