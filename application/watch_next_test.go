package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The round is oldest first, then by id, each question once; a new round
// starts again from the oldest.
func TestTheRoundIsOldestFirstAndEachQuestionOnce(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	first := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(time.Minute)
	second := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Which release fixed it?")
	h.clock.now = t0.Add(7 * time.Minute)
	got := watchNext(t, h, watcher.ExecutionID, "")
	if got.Done || got.Team != "qa" || got.Cursor != first.TaskID || got.View.Question != first.TaskID || !strings.Contains(got.View.Text(), "from qa-jefe to team dev") {
		t.Fatalf("first = %+v", got)
	}
	if got = watchNext(t, h, watcher.ExecutionID, got.Cursor); got.Done || got.Cursor != second.TaskID {
		t.Fatalf("second = %+v", got)
	}
	if got = watchNext(t, h, watcher.ExecutionID, got.Cursor); !got.Done || got.Team != "qa" {
		t.Fatalf("after the last = %+v", got)
	}
	if again := watchNext(t, h, watcher.ExecutionID, ""); again.Cursor != first.TaskID {
		t.Errorf("a new round = %+v", again)
	}
	if _, err := NewWatchNext(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, "task-none"); !domain.IsNotFound(err) {
		t.Errorf("an unknown cursor: %v", err)
	}
	// Ties in age go by id.
	a, b := mustTask(t, h, first.TaskID), mustTask(t, h, second.TaskID)
	if watchOrder(a, a) != 0 || watchOrder(a, b) >= 0 || watchOrder(b, a) <= 0 {
		t.Error("the order is not oldest first")
	}
}

// A question younger than WatchAfter is not offered, and no decision
// touches it.
func TestAYoungQuestionIsNotOffered(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(WatchAfter - time.Second)
	if got := watchNext(t, h, watcher.ExecutionID, ""); !got.Done {
		t.Errorf("a young question was offered: %+v", got)
	}
	_, err := NewDecideWatch(h.deps).Execute(context.Background(), "jev-node", watcher.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "abandon", Probability: 0.9, Confidence: 0.8})
	var state domain.StateError
	if !errors.As(err, &state) || !strings.Contains(err.Error(), "younger than 5m0s") {
		t.Errorf("abandon a young question: %v", err)
	}
	if status := mustTask(t, h, q.TaskID).Status(); status != domain.TaskPending {
		t.Errorf("the question is %s", status)
	}
	h.clock.now = t0.Add(WatchAfter)
	if got := watchNext(t, h, watcher.ExecutionID, ""); got.Cursor != q.TaskID {
		t.Errorf("at WatchAfter = %+v", got)
	}
}
