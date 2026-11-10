package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The operator writes both sides of the team channel in a definition or
// a patch: the asker's ask and the responder's question trigger.
func TestABotThatAsksAndAnswersIsDefinedAndPatched(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := sampleDefinition()
	d.ID = "qa-jefe"
	d.Groups = []string{"qa"}
	d.Ask = &domain.AskSnapshot{Teams: []string{"qa"}}
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil {
		t.Error("a bot asking its own team was registered")
	}
	d.Ask = &domain.AskSnapshot{Teams: []string{"dev"}, PerMinute: 11}
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil {
		t.Error("perMinute 11 was taken")
	}
	d.Ask = &domain.AskSnapshot{Teams: []string{"dev"}}
	d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "question", From: []string{"dev", "producto"}}}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil || !bot.Ask().Allows("dev") || bot.Ask().PerMinute() != domain.DefaultAskPerMinute || !bot.AnswersQuestionsFrom("producto") {
		t.Fatalf("registered: %v %+v", err, bot.Snapshot().Ask)
	}
	stored, err := h.deps.Bots.Get(ctx, "qa-jefe")
	if err != nil || stored.Snapshot().Ask == nil || stored.Snapshot().Ask.Teams[0] != "dev" {
		t.Fatalf("stored: %v %+v", err, stored.Snapshot().Ask)
	}

	patched, err := NewUpdateBot(h.deps).Execute(ctx, "qa-jefe", BotPatch{Ask: &domain.AskSnapshot{Teams: []string{"dev", "producto"}, MaxOpen: 5}})
	if err != nil || !patched.Ask().Allows("producto") || patched.Ask().MaxOpen() != 5 {
		t.Fatalf("patch: %v %+v", err, patched.Snapshot().Ask)
	}
	groups := []string{"qa", "dev"}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "qa-jefe", BotPatch{Groups: &groups}); err == nil {
		t.Error("a patch that joins the team the bot asks was taken")
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "qa-jefe", BotPatch{Ask: &domain.AskSnapshot{Teams: []string{"qa"}}}); err == nil {
		t.Error("a patch to ask its own team was taken")
	}
	cleared, err := NewUpdateBot(h.deps).Execute(ctx, "qa-jefe", BotPatch{Ask: &domain.AskSnapshot{}})
	if err != nil || cleared.Ask().Enabled() || cleared.Snapshot().Ask != nil {
		t.Fatalf("cleared: %v %+v", err, cleared.Snapshot().Ask)
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "qa-jefe", BotPatch{Groups: &groups}); err != nil {
		t.Errorf("with no ask, the groups change: %v", err)
	}
}

func TestNoBotIsRegisteredAsTheChannel(t *testing.T) {
	h := newHarness()
	d := sampleDefinition()
	d.ID = "canal"
	if _, err := NewRegisterBot(h.deps).Execute(context.Background(), d); err == nil {
		t.Error("a bot named canal was registered")
	}
	d.ID, d.Name = "", "canal"
	if _, err := NewRegisterBot(h.deps).Execute(context.Background(), d); err == nil {
		t.Error("a bot whose id defaults to canal was registered")
	}
}
