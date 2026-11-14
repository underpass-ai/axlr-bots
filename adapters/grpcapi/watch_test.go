package grpcapi

import (
	"context"
	"testing"
	"time"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

func TestBotWatchReplaysThenFollows(t *testing.T) {
	f := newFixture(t, "completed")
	bots := operatorv1.NewBotsServiceClient(f.authed)
	registerAndEnable(t, f, "bot-w", "observe")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := bots.WatchBotEvents(ctx, &operatorv1.WatchBotEventsRequest{BotId: "bot-w", ReplayLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetEvent().GetBotId() != "bot-w" {
		t.Fatalf("replay: %v %v", first, err)
	}
	mission := "changed while watching"
	if _, err := bots.UpdateBot(context.Background(), &operatorv1.UpdateBotRequest{BotId: "bot-w", Patch: &operatorv1.BotPatch{Mission: &mission}}); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		event *operatorv1.WatchBotEventsResponse
		err   error
	}
	next := make(chan answer, 1)
	go func() { e, err := stream.Recv(); next <- answer{e, err} }()
	select {
	case a := <-next:
		if a.err != nil || a.event.GetEvent().GetType() != "bot.updated" {
			t.Fatalf("follow: %v %v", a.event, a.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not hear the update")
	}
	cancel()
	if _, err := stream.Recv(); err == nil {
		t.Error("a cancelled watch went on")
	}
}

func TestWatchesRefuseWhatListsRefuse(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	bots := operatorv1.NewBotsServiceClient(f.authed)
	stream, _ := bots.WatchBotEvents(ctx, &operatorv1.WatchBotEventsRequest{BotId: "bot-w", ReplayLimit: -1})
	if _, err := stream.Recv(); err == nil {
		t.Error("a negative replay was taken")
	} else if k, _ := kind(t, err); k != "validation" {
		t.Errorf("negative replay: %v", err)
	}
	stream, _ = bots.WatchBotEvents(ctx, &operatorv1.WatchBotEventsRequest{BotId: "nobody"})
	if _, err := stream.Recv(); err == nil {
		t.Error("watched an unknown bot")
	} else if k, c := kind(t, err); k != "not_found" || c != "NotFound" {
		t.Errorf("unknown bot: %s %s", k, c)
	}
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	taskStream, _ := tasks.WatchTaskEvents(ctx, &operatorv1.WatchTaskEventsRequest{TaskId: "task-nope"})
	if _, err := taskStream.Recv(); err == nil {
		t.Error("watched an unknown task")
	} else if k, _ := kind(t, err); k != "not_found" {
		t.Errorf("unknown task: %v", err)
	}
}

func TestTaskWatchStreamsAfterTheCursorAndFollows(t *testing.T) {
	f := newFixture(t, "completed")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	registerAndEnable(t, f, "bot-a", "observe")
	submitted, err := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "say hi", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	id := submitted.GetTask().GetId()
	stream, err := tasks.WatchTaskEvents(ctx, &operatorv1.WatchTaskEventsRequest{TaskId: id})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetEvent().GetTaskId() != id {
		t.Fatalf("first: %v %v", first, err)
	}
	done := make(chan []string, 1)
	go func() {
		var types []string
		for {
			message, err := stream.Recv()
			if err != nil {
				done <- types
				return
			}
			types = append(types, message.GetEvent().GetType())
			if message.GetEvent().GetType() == "bot.task.completed" {
				done <- types
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := tasks.RunTask(context.Background(), &operatorv1.RunTaskRequest{TaskId: id}); err != nil {
		t.Fatal(err)
	}
	select {
	case types := <-done:
		if len(types) == 0 || types[len(types)-1] != "bot.task.completed" {
			t.Errorf("the watch saw %v", types)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not follow the run")
	}
}
