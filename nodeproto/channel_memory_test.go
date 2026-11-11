package nodeproto

import (
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAChannelRecordTravelsOnTheAnswersRun(t *testing.T) {
	record := domain.ChannelRecord{
		Side: domain.ChannelAsked, Team: "qa", Other: "dev", Question: "task-q1", Asker: "qa-jefe", Responder: "dev-go",
		Text: "Who owns the importer?", Answer: "deadline", Status: domain.TaskFailed,
	}
	objective, _ := domain.NewObjective("answer")
	assignment := application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun, LeaseUntil: time.Unix(1, 0), Run: &application.ExecutionRequest{
		TaskID: "task-a", BotID: "qa-jefe", Objective: objective, Level: domain.AutonomyObserve, ChannelMemory: &record,
	}}
	message := FromAssignment(assignment)
	if message.Run.ChannelMemory == nil || message.Run.ChannelMemory.Question != "task-q1" {
		t.Fatalf("message %+v", message.Run.ChannelMemory)
	}
	back, err := ToAssignment(message)
	if err != nil || back.Run.ChannelMemory == nil || *back.Run.ChannelMemory != record {
		t.Fatalf("back %+v %v", back.Run, err)
	}
	// A record that does not validate is dropped; the run stands.
	message.Run.ChannelMemory.Team = "dev"
	back, err = ToAssignment(message)
	if err != nil || back.Run.ChannelMemory != nil {
		t.Fatalf("a bad record: %+v %v", back.Run.ChannelMemory, err)
	}
	if FromChannelRecord(nil) != nil || ToChannelRecord(nil) != nil {
		t.Fatal("nil is not nil")
	}
}
