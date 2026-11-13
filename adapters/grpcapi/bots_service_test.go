package grpcapi

import (
	"context"
	"testing"

	"google.golang.org/protobuf/types/known/durationpb"
	"time"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestBotsServiceCarriesTheLifecycle(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	bots := operatorv1.NewBotsServiceClient(f.authed)

	def := definition("bot-t", "observe")
	def.Triggers = []*operatorv1.Trigger{
		{Type: "manual"},
		{Type: "webhook", Name: "deploy", Objective: "Handle {{.type}}", Secret: "whsec-0123456789abcdef"},
		{Type: "timer", Name: "tick", Objective: "say the time", Every: durationpb.New(10 * time.Minute)},
	}
	def.Groups = []string{"ops"}
	registered, err := bots.RegisterBot(ctx, &operatorv1.RegisterBotRequest{Definition: def})
	if err != nil {
		t.Fatal(err)
	}
	bot := registered.GetBot()
	if bot.GetStatus() != "Registered" || len(bot.GetTriggers()) != 3 || bot.GetTriggers()[1].GetSecret() != domain.Redacted {
		t.Errorf("register: %+v", bot)
	}
	if bot.GetTriggers()[2].GetEvery().AsDuration() != 10*time.Minute || bot.GetBudgets().GetTaskTimeout().AsDuration() != time.Minute {
		t.Errorf("durations: %+v", bot)
	}
	if stored, _ := f.deps.Bots.Get(ctx, "bot-t"); stored.Snapshot().Triggers[1].Secret != "whsec-0123456789abcdef" {
		t.Error("the repository should keep the secret")
	}
	if _, err := bots.RegisterBot(ctx, &operatorv1.RegisterBotRequest{Definition: def}); err == nil {
		t.Error("a duplicate was taken")
	} else if kind, code := kind(t, err); kind != "conflict" && kind != "validation" && kind != "state" {
		t.Errorf("duplicate: %s %s", kind, code)
	}

	list, err := bots.ListBots(ctx, &operatorv1.ListBotsRequest{})
	if err != nil || len(list.GetBots()) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	if got, err := bots.GetBot(ctx, &operatorv1.GetBotRequest{BotId: "bot-t"}); err != nil || got.GetBot().GetId() != "bot-t" {
		t.Fatalf("get: %v %v", got, err)
	}
	_, err = bots.GetBot(ctx, &operatorv1.GetBotRequest{BotId: "nobody"})
	if k, c := kind(t, err); k != "not_found" || c != "NotFound" {
		t.Errorf("get unknown: %s %s", k, c)
	}
	if detail := errorDetail(t, err); detail.GetResource() == "" || detail.GetId() != "nobody" {
		t.Errorf("detail: %+v", detail)
	}

	if enabled, err := bots.EnableBot(ctx, &operatorv1.EnableBotRequest{BotId: "bot-t"}); err != nil || enabled.GetBot().GetStatus() != "Enabled" {
		t.Fatalf("enable: %v %v", enabled, err)
	}
	if paused, err := bots.PauseBot(ctx, &operatorv1.PauseBotRequest{BotId: "bot-t"}); err != nil || paused.GetBot().GetStatus() != "Paused" {
		t.Fatalf("pause: %v %v", paused, err)
	}

	mission := "A patched mission"
	patched, err := bots.UpdateBot(ctx, &operatorv1.UpdateBotRequest{BotId: "bot-t", Patch: &operatorv1.BotPatch{Mission: &mission, Groups: &operatorv1.StringList{Values: []string{"ops", "extra"}}}})
	if err != nil || patched.GetBot().GetMission() != mission || len(patched.GetBot().GetGroups()) != 2 || patched.GetBot().GetTriggers()[1].GetSecret() != domain.Redacted {
		t.Fatalf("patch: %v %v", patched, err)
	}
	cleared, err := bots.UpdateBot(ctx, &operatorv1.UpdateBotRequest{BotId: "bot-t", Patch: &operatorv1.BotPatch{Groups: &operatorv1.StringList{}}})
	if err != nil || len(cleared.GetBot().GetGroups()) != 0 {
		t.Errorf("an empty list should clear: %v %v", cleared, err)
	}
	_, err = bots.UpdateBot(ctx, &operatorv1.UpdateBotRequest{BotId: "bot-t", Patch: &operatorv1.BotPatch{}})
	if k, c := kind(t, err); k != "validation" || c != "InvalidArgument" {
		t.Errorf("empty patch: %s %s", k, c)
	}

	events, err := bots.ListBotEvents(ctx, &operatorv1.ListBotEventsRequest{BotId: "bot-t"})
	if err != nil || len(events.GetEvents()) < 3 || events.GetEvents()[0].GetBotId() != "bot-t" {
		t.Fatalf("events: %v %v", events, err)
	}
	if two, _ := bots.ListBotEvents(ctx, &operatorv1.ListBotEventsRequest{BotId: "bot-t", Limit: 2}); len(two.GetEvents()) != 2 {
		t.Errorf("limited events: %v", two)
	}
	if _, err := bots.ListBotEvents(ctx, &operatorv1.ListBotEventsRequest{BotId: "bot-t", Limit: -1}); err == nil {
		t.Error("a negative limit was taken")
	}
	if _, err := bots.ListBotEvents(ctx, &operatorv1.ListBotEventsRequest{BotId: "nobody"}); err == nil {
		t.Error("events of an unknown bot")
	}
}

func TestBotsServiceSubmitsTasksStopsAndDeletes(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	bots := operatorv1.NewBotsServiceClient(f.authed)
	registerAndEnable(t, f, "bot-a", "observe")

	first, err := bots.SubmitBotTask(ctx, &operatorv1.SubmitBotTaskRequest{BotId: "bot-a", Objective: "look around", IdempotencyKey: "k-1", CorrelationId: "corr-1"})
	if err != nil || !first.GetCreated() || first.GetTask().GetBotId() != "bot-a" || first.GetTask().GetCorrelationId() != "corr-1" {
		t.Fatalf("submit: %v %v", first, err)
	}
	again, err := bots.SubmitBotTask(ctx, &operatorv1.SubmitBotTaskRequest{BotId: "bot-a", Objective: "look around", IdempotencyKey: "k-1"})
	if err != nil || again.GetCreated() || again.GetTask().GetId() != first.GetTask().GetId() {
		t.Errorf("same key: %v %v", again, err)
	}
	if _, err := bots.SubmitBotTask(ctx, &operatorv1.SubmitBotTaskRequest{BotId: "nobody", Objective: "x", IdempotencyKey: "k-2"}); err == nil {
		t.Error("a task for an unknown bot")
	}

	stopped, err := bots.StopBot(ctx, &operatorv1.StopBotRequest{BotId: "bot-a"})
	if err != nil || stopped.GetBot().GetStatus() != "Paused" || len(stopped.GetCancelled()) != 1 {
		t.Fatalf("stop: %v %v", stopped, err)
	}
	deleted, err := bots.DeleteBot(ctx, &operatorv1.DeleteBotRequest{BotId: "bot-a"})
	if err != nil || deleted.GetBot().GetId() != "bot-a" {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if _, err := bots.GetBot(ctx, &operatorv1.GetBotRequest{BotId: "bot-a"}); err == nil {
		t.Error("the bot is still there")
	}
}

func TestBotsServiceStagesAChange(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	bots := operatorv1.NewBotsServiceClient(f.authed)
	registerAndEnable(t, f, "bot-s", "observe")

	if _, err := bots.GetStagedBot(ctx, &operatorv1.GetStagedBotRequest{BotId: "bot-s"}); err == nil {
		t.Fatal("nothing is staged yet")
	}
	mission, version := "A staged mission", "2.0.0"
	staged, err := bots.StageBot(ctx, &operatorv1.StageBotRequest{BotId: "bot-s", Patch: &operatorv1.BotPatch{Version: &version, Mission: &mission,
		Triggers: &operatorv1.TriggerList{Values: []*operatorv1.Trigger{{Type: "webhook", Name: "w", Objective: "x", Secret: "whsec-0123456789abcdef"}}}}})
	if err != nil || staged.GetBot().GetStaged().GetVersion() != "2.0.0" || len(staged.GetBot().GetStaged().GetChange()) != 0 {
		t.Fatalf("stage: %v %v", staged, err)
	}
	view, err := bots.GetStagedBot(ctx, &operatorv1.GetStagedBotRequest{BotId: "bot-s"})
	if err != nil || view.GetVersion() != "2.0.0" || view.GetPatch().GetMission() != mission || view.GetPatch().GetTriggers().GetValues()[0].GetSecret() != domain.Redacted {
		t.Fatalf("staged: %v %v", view, err)
	}
	applied, err := bots.ApplyStagedBot(ctx, &operatorv1.ApplyStagedBotRequest{BotId: "bot-s"})
	if err != nil || applied.GetBot().GetMission() != mission || applied.GetBot().GetStaged() != nil {
		t.Fatalf("apply: %v %v", applied, err)
	}
	next := "3.0.0"
	if _, err := bots.StageBot(ctx, &operatorv1.StageBotRequest{BotId: "bot-s", Patch: &operatorv1.BotPatch{Version: &next, Mission: &mission}}); err != nil {
		t.Fatal(err)
	}
	unstaged, err := bots.UnstageBot(ctx, &operatorv1.UnstageBotRequest{BotId: "bot-s"})
	if err != nil || unstaged.GetBot().GetStaged() != nil {
		t.Fatalf("unstage: %v %v", unstaged, err)
	}
	if _, err := bots.ApplyStagedBot(ctx, &operatorv1.ApplyStagedBotRequest{BotId: "bot-s"}); err == nil {
		t.Error("applied what was dropped")
	}
}
