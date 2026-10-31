package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func jevDefinition() BotDefinition {
	d := sampleDefinition()
	d.Name = "ob-1"
	d.Level = "act-within-policy"
	d.AllowedTools = []string{"phrases", "play", "listen", "stop"}
	d.Budgets.MaxDelegationDepth = 0
	d.Runtime = &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
		Server: "score", Start: "phrases", Wait: "listen", Stop: "stop",
		Act: domain.JevActSnapshot{Tool: "play", Choice: "phrase", Verdict: "judge", Pass: []string{"dynamics"}},
	}}
	return d
}

// A Jev bot is registered with its program, which its tools and level
// must carry; an update that narrows them past the program is refused;
// its tasks are assigned only to a node with the jev runtime, and the
// assignment carries the runtime and the task's input.
func TestAJevBotRunsOnlyWhereJevIs(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	observing := jevDefinition()
	observing.Level = "observe"
	if _, err := NewRegisterBot(h.deps).Execute(ctx, observing); err == nil || !strings.Contains(err.Error(), "act-within-policy") {
		t.Errorf("an observing Jev bot: %v", err)
	}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, jevDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if bot.Runtime().Kind() != domain.RuntimeJev || bot.Snapshot().Runtime == nil {
		t.Fatalf("runtime = %v", bot.Runtime().Kind())
	}
	fewer := []string{"phrases", "play"}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{AllowedTools: &fewer}); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Errorf("narrowing the tools past the program: %v", err)
	}
	observe := "observe"
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Level: &observe}); err == nil {
		t.Error("lowering the level under a program was accepted")
	}
	broken := &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{Server: "score", Start: "tune", Wait: "listen", Act: domain.JevActSnapshot{Tool: "play", Choice: "phrase"}}}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Runtime: broken}); err == nil || !strings.Contains(err.Error(), "tune") {
		t.Errorf("a program over a tool the bot lacks: %v", err)
	}
	bot, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	plain, err := NewRegisterNode(h.deps).Execute(ctx, registration("plain-1"))
	if err != nil {
		t.Fatal(err)
	}
	withJev := registration("jev-1")
	withJev.Capabilities.Runtimes = []string{"jev"}
	jevNode, err := NewRegisterNode(h.deps).Execute(ctx, withJev)
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(time.Second)
	submitted := submit(t, h, bot, "k-cue").Task
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, plain.ID()); err != nil || found {
		t.Errorf("a node without the runtime was assigned a Jev bot: %v %v", found, err)
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, jevNode.ID())
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != submitted.ID() {
		t.Fatalf("assignment = %+v %v %v", assignment, found, err)
	}
	program, ok := assignment.Run.Runtime.Jev()
	if !ok || program.Act() != "play" || string(assignment.Run.Input) != string(submitted.Input()) {
		t.Errorf("run = %+v", assignment.Run)
	}
	// Back to AXLR: the runtime is dropped from the snapshot.
	axlr := &domain.RuntimeSnapshot{Kind: "axlr"}
	bot, err = NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Runtime: axlr})
	if err != nil || bot.Runtime().Kind() != domain.RuntimeAXLR || bot.Snapshot().Runtime != nil {
		t.Errorf("back to axlr: %v %+v", err, bot.Snapshot().Runtime)
	}
	// Now the narrower tools pass.
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{AllowedTools: &fewer}); err != nil {
		t.Errorf("narrowing without a program: %v", err)
	}
}

// A teammate's or the operator's event delivered as a message carries
// its name and data, so a Jev program reads the cue without parsing text.
func TestAMessageCarriesItsEvent(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := jevDefinition()
	d.Groups = []string{"orchestra"}
	d.Subscriptions = []domain.SubscriptionSnapshot{{Topic: "info.cue.winds", Mode: "message", From: []string{"operator"}}}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	bot, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	withJev := registration("jev-1")
	withJev.Capabilities.Runtimes = []string{"jev"}
	node, _ := NewRegisterNode(h.deps).Execute(ctx, withJev)
	submit(t, h, bot, "k-first")
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found {
		t.Fatal(err, found)
	}
	answer, err := NewPublishGroupEvent(h.deps).Execute(ctx, PublishGroupEventCommand{Group: "orchestra", Name: "info.cue.winds", Message: "softer", Data: map[string]any{"dynamics": "p"}, IdempotencyKey: "cue-1"})
	if err != nil || len(answer.Deliveries) != 1 || answer.Deliveries[0].Mode != "message" {
		t.Fatalf("publish = %+v %v", answer, err)
	}
	messages, err := NewExecutionMessages(h.deps).Execute(ctx, node.ID(), assignment.ExecutionID, "")
	if err != nil || len(messages) != 1 || messages[0].Event == nil || messages[0].Event.Name != "info.cue.winds" || messages[0].Event.Data["dynamics"] != "p" || !strings.Contains(messages[0].Text, "softer") {
		t.Errorf("messages = %+v %v", messages, err)
	}
}
