package nodeproto

import (
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The next question of a round travels with its view and cursor; done
// travels alone; a round that is not done without a question, or with a
// bad cursor, does not read back.
func TestAWatchedQuestionRoundTrips(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	view := application.QuestionView{Question: "task-9", Team: "dev", AskedBy: "qa-jefe", Responder: "dev-a", Status: domain.TaskPending, AskedAt: at, Deadline: at.Add(2 * time.Hour), Now: at.Add(6 * time.Minute)}
	back, err := ToWatched(FromWatched(application.WatchedQuestion{Team: "qa", View: view, Cursor: "task-9"}))
	if err != nil || back.Done || back.Team != "qa" || back.Cursor != "task-9" || back.View.Question != "task-9" || back.View.Responder != "dev-a" {
		t.Fatalf("back = %+v %v", back, err)
	}
	done := FromWatched(application.WatchedQuestion{Done: true, Team: "qa"})
	if done.Question != nil || done.Cursor != "" {
		t.Errorf("done = %+v", done)
	}
	if back, err := ToWatched(done); err != nil || !back.Done || back.Team != "qa" {
		t.Errorf("done back = %+v %v", back, err)
	}
	if _, err := ToWatched(WatchedMessage{Team: "qa"}); err == nil {
		t.Error("a round with no question read back")
	}
	message := FromWatched(application.WatchedQuestion{Team: "qa", View: view, Cursor: "task-9"})
	message.Cursor = "Bad Id"
	if _, err := ToWatched(message); err == nil {
		t.Error("a bad cursor read back")
	}
	message.Question.Status = "Lost"
	if _, err := ToWatched(message); err == nil {
		t.Error("a bad view read back")
	}
}

// A team task of a round travels with its view; a round that names both a
// question and a task, or a task with a bad id, does not read back.
func TestAWatchedTaskRoundTrips(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	view := application.TaskWatchView{Task: "task-7", Team: "dev", Bot: "dev-go", Trigger: "timer cola", Objective: "Queue round", Now: at.Add(9 * time.Minute), RunningSince: at, Steps: 4, LastStep: "tool local_exec", LastStepAt: at.Add(2 * time.Minute), OpenTasks: 1}
	message := FromWatched(application.WatchedQuestion{Team: "dev", Task: &view, Cursor: "task-7"})
	if message.Question != nil || message.Task == nil {
		t.Fatalf("message = %+v", message)
	}
	back, err := ToWatched(message)
	if err != nil || back.Task == nil || *back.Task != view || back.Cursor != "task-7" {
		t.Fatalf("back = %+v %v", back, err)
	}
	quiet := FromWatched(application.WatchedQuestion{Team: "dev", Task: &application.TaskWatchView{Task: "task-8", Now: at}, Cursor: "task-8"})
	if back, err := ToWatched(quiet); err != nil || !back.Task.LastStepAt.IsZero() {
		t.Errorf("quiet = %+v %v", back, err)
	}
	both := message
	question := FromQuestionView(application.QuestionView{Question: "task-9", Status: domain.TaskPending})
	both.Question = &question
	if _, err := ToWatched(both); err == nil {
		t.Error("a round naming a question and a task read back")
	}
	bad := FromWatched(application.WatchedQuestion{Team: "dev", Task: &view, Cursor: "task-7"})
	bad.Task.Task = "Bad Id"
	if _, err := ToWatched(bad); err == nil {
		t.Error("a bad task id read back")
	}
}
