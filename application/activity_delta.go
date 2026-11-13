package application

import (
	"sort"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// stamped is an update with the instant of the change it tells.
type stamped struct {
	at     time.Time
	update ActivityUpdate
}

// activityDelta tells what in the tree is newer than since, oldest
// first: a task changed, an execution started or ended, an event, a
// child that joined (with everything known of it) and a progress cut
// found or completed.
func activityDelta(tree TaskActivity, since time.Time) []ActivityUpdate {
	var found []stamped
	collectDelta(tree, "", since, &found)
	sort.SliceStable(found, func(i, j int) bool { return found[i].at.Before(found[j].at) })
	out := make([]ActivityUpdate, len(found))
	for i, f := range found {
		f.update.Cursor = activityCursor(f.at)
		out[i] = f.update
	}
	return out
}

func collectDelta(node TaskActivity, parent domain.TaskID, since time.Time, found *[]stamped) {
	task := node.Task
	if parent != "" && task.CreatedAt().After(since) {
		child := node
		*found = append(*found, stamped{at: highWater(node), update: ActivityUpdate{Child: &ChildAttached{ParentTaskID: parent, Child: child}}})
		return
	}
	if task.UpdatedAt().After(since) {
		t := task
		*found = append(*found, stamped{at: task.UpdatedAt(), update: ActivityUpdate{Task: &t}})
	}
	for _, event := range node.Events {
		if event.CreatedAt.After(since) {
			*found = append(*found, stamped{at: event.CreatedAt, update: ActivityUpdate{Event: &ActivityEvent{TaskID: task.ID(), Event: event}}})
		}
	}
	for _, exec := range node.Executions {
		collectExecution(node, exec, since, found)
	}
	for _, child := range node.Children {
		collectDelta(child, task.ID(), since, found)
	}
}

func collectExecution(node TaskActivity, exec ExecutionActivity, since time.Time, found *[]stamped) {
	execution := exec.Execution
	if at := executionTime(execution); at.After(since) {
		e := execution
		*found = append(*found, stamped{at: at, update: ActivityUpdate{Execution: &e}})
	}
	if exec.Cut == nil {
		return
	}
	at := progressOf(node.Events, execution.ID()).lastEvent
	if end := execution.EndedAt(); end.After(at) {
		at = end
	}
	if at.After(since) {
		*found = append(*found, stamped{at: at, update: ActivityUpdate{Completed: &ProgressCompletion{
			TaskID: node.Task.ID(), ExecutionID: execution.ID(), Cut: *exec.Cut, Items: exec.Completed,
		}}})
	}
}

// executionTime is when the execution last changed in a way a watcher
// sees: it started, or it ended.
func executionTime(e domain.Execution) time.Time {
	if end := e.EndedAt(); end.After(e.StartedAt()) {
		return end
	}
	return e.StartedAt()
}

// highWater is the newest instant anything in the tree changed.
func highWater(node TaskActivity) time.Time {
	hw := node.Task.UpdatedAt()
	if c := node.Task.CreatedAt(); c.After(hw) {
		hw = c
	}
	for _, event := range node.Events {
		if event.CreatedAt.After(hw) {
			hw = event.CreatedAt
		}
	}
	for _, exec := range node.Executions {
		if t := executionTime(exec.Execution); t.After(hw) {
			hw = t
		}
	}
	for _, child := range node.Children {
		if t := highWater(child); t.After(hw) {
			hw = t
		}
	}
	return hw
}

// finished reports a tree that can change no further: every task ended
// and no execution runs.
func finished(node TaskActivity) bool {
	if !node.Task.Status().IsTerminal() {
		return false
	}
	for _, exec := range node.Executions {
		if !exec.Execution.Outcome().IsFinal() {
			return false
		}
	}
	for _, child := range node.Children {
		if !finished(child) {
			return false
		}
	}
	return true
}
