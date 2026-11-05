package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestTurningTheLevelSaysWhereFromAndWhereTo(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	from := bot.Level()
	to := string(domain.AutonomyObserve)
	if from == domain.AutonomyObserve {
		to = string(domain.AutonomyActWithApproval)
	}
	updated, err := NewUpdateBot(h.deps).Execute(context.Background(), bot.ID(), BotPatch{Level: &to})
	if err != nil || updated.Level().String() != to {
		t.Fatalf("level not turned: %v %v", updated.Level(), err)
	}
	event := h.log.events[len(h.log.events)-1]
	if event.Type != domain.EventBotUpdated || event.Payload["previousLevel"] != from.String() || event.Payload["level"] != to {
		t.Fatalf("event %v %v", event.Type, event.Payload)
	}

	// A patch that leaves the level as it was names no levels.
	mission := "Watch the queue and report."
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), bot.ID(), BotPatch{Mission: &mission}); err != nil {
		t.Fatal(err)
	}
	event = h.log.events[len(h.log.events)-1]
	if _, named := event.Payload["level"]; named {
		t.Fatalf("an unchanged level is named: %v", event.Payload)
	}
}

func TestAutoApproveChangesOnlyTheCallsApprovedWithoutAPerson(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := governBot(t, h, enabledBot(t, h), domain.ApprovalsSnapshot{TTL: "2h", Notify: []domain.NotifyTargetSnapshot{
		{Type: "webhook", Name: "ops", URL: "https://hooks.example.com/axlr", Secret: "a-secret-of-sixteen-plus"},
	}})
	rules := AutoApproveRules{{Tool: "kubernetes.get_pods"}}
	updated, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{AutoApprove: &rules})
	if err != nil {
		t.Fatal(err)
	}
	approvals := updated.Approvals().Snapshot()
	if len(approvals.AutoApprove) != 1 || approvals.TTL != "2h" || approvals.Notify[0].Secret != "a-secret-of-sixteen-plus" {
		t.Fatalf("approvals = %+v", approvals)
	}
	if event := h.log.events[len(h.log.events)-1]; event.Payload["fields"].([]string)[0] != "autoApprove" {
		t.Errorf("event = %+v", event.Payload)
	}
	bad := AutoApproveRules{{Tool: ""}}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{AutoApprove: &bad}); err == nil {
		t.Error("a bad rule was taken")
	}
}
