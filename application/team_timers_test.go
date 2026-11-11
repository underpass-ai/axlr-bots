package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// teamRound declares the team's timer cola every 10 minutes.
func teamRound(every string) func(*BotDefinition) {
	return func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "timer", Name: "cola", Objective: "queue round at {{.scheduledAt}}", Every: every, Deliver: "team"}}
	}
}

func devTeam(t *testing.T, h *harness) {
	t.Helper()
	onlineNode(t, h, "web-1")
	for _, id := range []string{"dev-jefe", "dev-go", "dev-revisor"} {
		teamBot(t, h, id, []string{"dev"}, teamRound("10m"))
	}
}

func TestATeamTimerFiresOnceForOneMember(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	devTeam(t, h)
	uc := NewTickTimers(h.deps)
	result, err := uc.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 || len(result.Fired) != 1 {
		t.Fatalf("first pass = %+v", result)
	}
	fire := result.Fired[0]
	if fire.BotID != "dev-go" || fire.Team != "dev" || fire.Trigger != "cola" || !fire.Created {
		t.Errorf("fire = %+v", fire)
	}
	timer, err := h.timers.Get(ctx, domain.TeamTimerKey("dev", "cola"))
	if err != nil || timer.Team() != "dev" || timer.BotID() != "" || timer.LastTaskID() != fire.TaskID {
		t.Fatalf("timer = %+v %v", timer.Snapshot(), err)
	}
	if n := countTasks(h, domain.TriggerTimer); n != 1 {
		t.Errorf("%d timer tasks, want 1", n)
	}
	// The next slot: dev-go still has its round open, so dev-jefe takes it.
	h.clock.now = t0.Add(10 * time.Minute)
	result, _ = uc.Execute(ctx)
	if len(result.Fired) != 1 || result.Fired[0].BotID != "dev-jefe" {
		t.Errorf("second slot = %+v", result.Fired)
	}
	// A crash between the fire and the save: the slot finds its task.
	timer, _ = h.timers.Get(ctx, domain.TeamTimerKey("dev", "cola"))
	last := timer.LastTaskID()
	_ = timer.Rearm(timer.ScheduleKey(), t0.Add(10*time.Minute))
	_ = h.timers.Save(ctx, timer)
	result, _ = uc.Execute(ctx)
	if len(result.Fired) != 1 || result.Fired[0].Created || result.Fired[0].TaskID != last || result.Fired[0].BotID != "dev-jefe" {
		t.Errorf("replayed slot = %+v", result.Fired)
	}
	if n := countTasks(h, domain.TriggerTimer); n != 2 {
		t.Errorf("%d timer tasks, want 2", n)
	}
}

func TestATeamTimerWithTwoSchedulesFiresNothing(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	teamBot(t, h, "dev-go", []string{"dev"}, teamRound("10m"))
	teamBot(t, h, "dev-jefe", []string{"dev"}, teamRound("20m"))
	result, err := NewTickTimers(h.deps).Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fired) != 0 || len(result.Errors) != 1 || result.Errors[0].Team != "dev" || !strings.Contains(result.Errors[0].Err.Error(), "different schedules") {
		t.Errorf("result = %+v", result)
	}
}

func TestATeamTimerNobodyTakesIsAdvancedAndDroppedWhenUndeclared(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	devTeam(t, h)
	uc := NewTickTimers(h.deps)
	if _, err := uc.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.BotID{"dev-jefe", "dev-go", "dev-revisor"} {
		if _, err := NewPauseBot(h.deps).Execute(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	h.clock.now = t0.Add(10 * time.Minute)
	if result, _ := uc.Execute(ctx); len(result.Fired) != 0 {
		t.Errorf("fired with nobody taking tasks: %+v", result.Fired)
	}
	timer, _ := h.timers.Get(ctx, domain.TeamTimerKey("dev", "cola"))
	if !timer.NextFireAt().After(h.clock.now) {
		t.Errorf("not advanced: %s", timer.NextFireAt())
	}
	for _, id := range []string{"dev-jefe", "dev-go", "dev-revisor"} {
		triggers := []domain.TriggerSnapshot{{Type: "manual"}}
		if _, err := NewUpdateBot(h.deps).Execute(ctx, domain.BotID(id), BotPatch{Triggers: &triggers}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := uc.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.timers.Get(ctx, domain.TeamTimerKey("dev", "cola")); !domain.IsNotFound(err) {
		t.Errorf("team timer kept: %v", err)
	}
}
