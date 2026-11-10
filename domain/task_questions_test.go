package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// A task keeps the questions it asked apart from its children: they take
// nothing of its budgets, and a task stored before them reads as before.
func TestATaskKeepsItsQuestionsApartFromItsChildren(t *testing.T) {
	task, _ := NewTask("task-1", "qa-jefe", "Lead QA", "lead-1", "trace-1", t0)
	if err := task.AdoptQuestion("task-2", t0); err == nil {
		t.Error("a Pending task asked a team")
	}
	_ = task.Start("exec-1", t0)
	for _, child := range []TaskID{"", "task-1"} {
		if err := task.AdoptQuestion(child, t0); err == nil {
			t.Errorf("adopted %q", child)
		}
	}
	plain, _ := json.Marshal(task.Snapshot())
	if strings.Contains(string(plain), "questions") {
		t.Errorf("a task without questions: %s", plain)
	}
	if err := task.AdoptQuestion("task-2", t0.Add(1)); err != nil {
		t.Fatal(err)
	}
	if err := task.AdoptQuestion("task-3", t0.Add(2)); err != nil {
		t.Fatal(err)
	}
	if q := task.Questions(); len(q) != 2 || q[1] != "task-3" || len(task.Children()) != 0 || !task.UpdatedAt().Equal(t0.Add(2)) {
		t.Errorf("questions %v children %v", q, task.Children())
	}
	if model, tools := task.Granted(); model != 0 || tools != 0 {
		t.Errorf("granted %d %d", model, tools)
	}
	got := task.Questions()
	got[0] = "changed"
	if task.Questions()[0] != "task-2" {
		t.Error("the questions are shared")
	}
	restored, err := RestoreTask(task.Snapshot())
	if err != nil || len(restored.Questions()) != 2 || restored.Questions()[0] != "task-2" {
		t.Fatalf("restored: %v %v", err, restored.Questions())
	}
	broken := task.Snapshot()
	broken.Questions = []string{"Bad Id"}
	if _, err := RestoreTask(broken); err == nil {
		t.Error("restored a bad question id")
	}
}
