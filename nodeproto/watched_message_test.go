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
