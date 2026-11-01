package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// teamBot registers and enables a bot of tenant default in the teams
// given, taking manual tasks only: a teammate delegates to it through the
// team, not through a delegation trigger.
func teamBot(t *testing.T, h *harness, id string, groups []string, mutate func(*BotDefinition)) domain.Bot {
	t.Helper()
	ctx := context.Background()
	d := sampleDefinition()
	d.ID, d.Name, d.Tenant = id, id, "default"
	d.Mission = "You are " + id + ". You do what each task asks."
	d.AllowedTools = []string{"local_read"}
	d.Groups = groups
	d.Budgets = BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 10, TaskTimeoutSeconds: 300}
	if mutate != nil {
		mutate(&d)
	}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	return bot
}

func TestRegisterAndPatchTeams(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := teamBot(t, h, "watcher", []string{"ops", "infra"}, nil)
	if got := bot.Snapshot().Groups; len(got) != 2 || got[0] != "infra" || got[1] != "ops" {
		t.Fatalf("groups = %v", got)
	}
	groups := []string{"ops"}
	bot, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Groups: &groups})
	if err != nil || len(bot.Teams().Names()) != 1 {
		t.Fatalf("patch: %v %v", bot.Snapshot().Groups, err)
	}
	if event, ok := lastEvent(h, domain.EventBotUpdated); !ok || event.Payload["fields"].([]string)[0] != "groups" {
		t.Errorf("updated event = %+v", event)
	}
	bad := []string{"Ops"}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Groups: &bad}); err == nil {
		t.Error("a bad team name was accepted")
	}
	d := sampleDefinition()
	d.Groups = []string{"a", "a"}
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil {
		t.Error("a repeated team was accepted")
	}
}

func TestAssignmentCarriesTheDirectoryAndTheTeamAsTargets(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	watcher := teamBot(t, h, "watcher", []string{"ops"}, nil)
	teamBot(t, h, "worker-1", []string{"ops"}, func(d *BotDefinition) { d.Placement = domain.PlacementSnapshot{Nodes: []string{"worker-1"}} })
	teamBot(t, h, "worker-2", []string{"ops"}, func(d *BotDefinition) {
		d.Delegation = &domain.DelegationSnapshot{FromTeam: new(bool)}
	})
	paused := teamBot(t, h, "worker-3", []string{"ops"}, nil)
	if _, err := NewPauseBot(h.deps).Execute(ctx, paused.ID()); err != nil {
		t.Fatal(err)
	}
	teamBot(t, h, "stranger", []string{"other"}, nil)
	assignment := runOnNode(t, h, watcher, "web-1", "ask-1")
	run := assignment.Run
	if run == nil || !contains(run.AllowedTools, DelegateToolName) {
		t.Fatalf("run = %+v", run)
	}
	if offer := run.Delegation; offer == nil || strings.Join(offer.Targets, ",") != "worker-1" {
		t.Fatalf("targets = %+v", run.Delegation)
	}
	prompt := run.Prompt.String()
	for _, want := range []string{"Your teammates (team ops)", "- worker-1: You are worker-1. Tools: local_read. Runs on node worker-1. Takes delegated tasks.", "- worker-2: You are worker-2. Tools: local_read. Runs on any node.\n", "The task:\nFind out"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"worker-3", "stranger"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("prompt names %s:\n%s", unwanted, prompt)
		}
	}
}

func TestABotWithoutTeamsSeesNoDirectory(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	alone := teamBot(t, h, "alone", nil, nil)
	run := runOnNode(t, h, alone, "web-1", "ask-1").Run
	if run.Delegation != nil || contains(run.AllowedTools, DelegateToolName) || strings.Contains(run.Prompt.String(), "teammates") {
		t.Errorf("run = %+v", run)
	}
}

func TestDelegateInsideTheTeam(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	watcher := teamBot(t, h, "watcher", []string{"ops"}, nil)
	teamBot(t, h, "worker-1", []string{"ops"}, nil)
	teamBot(t, h, "worker-2", []string{"ops"}, func(d *BotDefinition) { d.Delegation = &domain.DelegationSnapshot{FromTeam: new(bool)} })
	teamBot(t, h, "stranger", []string{"other"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "delegation"}}
	})
	parent := runOnNode(t, h, watcher, "web-1", "ask-1")
	answer := delegate(t, h, "web-1", parent.ExecutionID, "worker-1", "Roll back")
	if answer.Refused != nil || !answer.Created {
		t.Fatalf("teammate: %+v", answer)
	}
	answer = delegate(t, h, "web-1", parent.ExecutionID, "worker-2", "Roll back")
	if answer.Refused == nil || answer.Refused.Reason != domain.RefusedTargetUnavailable || !strings.Contains(answer.Refused.Message, "fromTeam false") {
		t.Errorf("refusing teammate: %+v", answer.Refused)
	}
	answer = delegate(t, h, "web-1", parent.ExecutionID, "stranger", "Roll back")
	if answer.Refused == nil || answer.Refused.Reason != domain.RefusedNotAllowed || !strings.Contains(answer.Refused.Message, "it may ask: worker-1") {
		t.Errorf("stranger: %+v", answer.Refused)
	}
	answer = delegate(t, h, "web-1", parent.ExecutionID, "ghost", "Roll back")
	if answer.Refused == nil || answer.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("ghost: %+v", answer.Refused)
	}
}

func TestDelegationFailsWhenTheBotsCannotBeListed(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	watcher := teamBot(t, h, "watcher", []string{"ops"}, nil)
	teamBot(t, h, "worker-1", []string{"ops"}, nil)
	parent := runOnNode(t, h, watcher, "web-1", "ask-1")
	h.bots.listFail = errBoom
	if _, err := NewDelegateTask(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, DelegateCommand{Bot: "ghost", Objective: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
	if _, err := NewSubmitTask(h.deps).Execute(context.Background(), SubmitTaskCommand{BotID: "watcher", Objective: "again", IdempotencyKey: "ask-2"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewNextAssignment(h.deps).Execute(context.Background(), "web-1"); !errors.Is(err, errBoom) {
		t.Errorf("assignment err = %v", err)
	}
}

func TestListAndGetGroups(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	teamBot(t, h, "worker-1", []string{"ops"}, nil)
	teamBot(t, h, "watcher", []string{"ops", "infra"}, func(d *BotDefinition) {
		d.Mission = "Watch service-1 at dawn. Then report."
		d.Delegation = &domain.DelegationSnapshot{FromTeam: new(bool)}
	})
	teamBot(t, h, "alone", nil, nil)
	teamBot(t, h, "other-ops", []string{"ops"}, func(d *BotDefinition) { d.Tenant = "staging" })
	groups, err := NewListGroups(h.deps).Execute(ctx)
	if err != nil || len(groups) != 3 {
		t.Fatalf("groups = %+v %v", groups, err)
	}
	if groups[0].TenantID != "default" || groups[0].Name != "infra" || groups[1].Name != "ops" || groups[2].TenantID != "staging" {
		t.Errorf("order = %+v", groups)
	}
	ops := groups[1]
	if len(ops.Members) != 2 || ops.Members[0].BotID != "watcher" || ops.Members[1].BotID != "worker-1" {
		t.Fatalf("ops = %+v", ops)
	}
	watcher := ops.Members[0]
	if watcher.Purpose != "Watch service-1 at dawn." || watcher.TakesDelegation || watcher.Status != "Enabled" || watcher.Where != "any node" || len(watcher.Groups) != 2 {
		t.Errorf("watcher = %+v", watcher)
	}
	if !ops.Members[1].TakesDelegation {
		t.Errorf("worker-1 = %+v", ops.Members[1])
	}
	if got, err := NewGetGroup(h.deps).Execute(ctx, "infra", ""); err != nil || got.Name != "infra" {
		t.Errorf("get infra = %+v %v", got, err)
	}
	if got, err := NewGetGroup(h.deps).Execute(ctx, "ops", "staging"); err != nil || len(got.Members) != 1 {
		t.Errorf("get staging ops = %+v %v", got, err)
	}
	var validation domain.ValidationError
	if _, err := NewGetGroup(h.deps).Execute(ctx, "ops", ""); !errors.As(err, &validation) || !strings.Contains(validation.Reason, "default, staging") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := NewGetGroup(h.deps).Execute(ctx, "sre", ""); !domain.IsNotFound(err) {
		t.Errorf("missing: %v", err)
	}
	if _, err := NewGetGroup(h.deps).Execute(ctx, "Sre", ""); err == nil {
		t.Error("a bad name was accepted")
	}
	if _, err := NewGetGroup(h.deps).Execute(ctx, "sre", "Bad Tenant"); err == nil {
		t.Error("a bad tenant was accepted")
	}
	h.bots.listFail = errBoom
	if _, err := NewGetGroup(h.deps).Execute(ctx, "ops", ""); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
}

func TestRunTaskInProcessReadsTheDirectory(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	watcher := teamBot(t, h, "watcher", []string{"ops"}, nil)
	teamBot(t, h, "worker-1", []string{"ops"}, nil)
	submitted, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: watcher.ID().String(), Objective: "Look at the queue", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunTask(h.deps).Execute(ctx, submitted.Task.ID()); err != nil {
		t.Fatal(err)
	}
	request := h.runtime.requests[len(h.runtime.requests)-1]
	if !strings.Contains(request.Prompt.String(), "- worker-1: You are worker-1.") || request.Delegation != nil {
		t.Errorf("request = %+v", request)
	}
}
