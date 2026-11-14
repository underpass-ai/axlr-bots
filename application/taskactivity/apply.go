package taskactivity

import (
	"github.com/underpass-ai/axlr-bots/domain"
)

// Apply folds one update into the tree. A snapshot replaces it; the
// other updates find their task or execution by id and change it, and
// one that names something the tree does not hold is left out.
func (a *Activity) Apply(update Update) {
	switch {
	case update.Snapshot != nil:
		*a = *update.Snapshot
	case update.TaskChanged != nil:
		if node := a.find(update.TaskChanged.ID); node != nil {
			node.Task = *update.TaskChanged
		}
	case update.ExecutionChanged != nil:
		if node := a.find(update.ExecutionChanged.TaskID); node != nil {
			node.setExecution(*update.ExecutionChanged)
		}
	case update.EventAppended != nil:
		if node := a.find(update.EventAppended.TaskID); node != nil {
			node.Events = append(node.Events, update.EventAppended.Event)
		}
	case update.ChildAttached != nil:
		if node := a.find(update.ChildAttached.ParentTaskID); node != nil && a.find(update.ChildAttached.Child.Task.ID) == nil {
			node.Children = append(node.Children, update.ChildAttached.Child)
		}
	case update.ProgressCompleted != nil:
		if node := a.find(update.ProgressCompleted.TaskID); node != nil {
			node.complete(*update.ProgressCompleted)
		}
	}
}

// find is the node of the task in this subtree.
func (a *Activity) find(taskID string) *Activity {
	if a.Task.ID == taskID {
		return a
	}
	for i := range a.Children {
		if node := a.Children[i].find(taskID); node != nil {
			return node
		}
	}
	return nil
}

func (a *Activity) setExecution(snapshot domain.ExecutionSnapshot) {
	for i := range a.Executions {
		if a.Executions[i].Snapshot.ID == snapshot.ID {
			a.Executions[i].Snapshot = snapshot
			return
		}
	}
	a.Executions = append(a.Executions, Execution{Snapshot: snapshot})
}

func (a *Activity) complete(done ProgressCompleted) {
	for i := range a.Executions {
		if a.Executions[i].Snapshot.ID != done.ExecutionID {
			continue
		}
		if done.Cut != nil {
			a.Executions[i].Cut = done.Cut
		}
		a.Executions[i].Completed = append(a.Executions[i].Completed, done.Items...)
		return
	}
}
