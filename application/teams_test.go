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
	bot := teamBot(t, h, "plane", []string{"port", "air"}, nil)
	if got := bot.Snapshot().Groups; len(got) != 2 || got[0] != "air" || got[1] != "port" {
		t.Fatalf("groups = %v", got)
	}
	groups := []string{"port"}
	bot, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Groups: &groups})
	if err != nil || len(bot.Teams().Names()) != 1 {
		t.Fatalf("patch: %v %v", bot.Snapshot().Groups, err)
	}
	if event, ok := lastEvent(h, domain.EventBotUpdated); !ok || event.Payload["fields"].([]string)[0] != "groups" {
		t.Errorf("updated event = %+v", event)
	}
	bad := []string{"Port"}
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
	plane := teamBot(t, h, "plane", []string{"port"}, nil)
	teamBot(t, h, "quad-1", []string{"port"}, func(d *BotDefinition) { d.Placement = domain.PlacementSnapshot{Nodes: []string{"quad-1"}} })
	teamBot(t, h, "quad-2", []string{"port"}, func(d *BotDefinition) {
		d.Delegation = &domain.DelegationSnapshot{FromTeam: new(bool)}
	})
	paused := teamBot(t, h, "quad-3", []string{"port"}, nil)
	if _, err := NewPauseBot(h.deps).Execute(ctx, paused.ID()); err != nil {
		t.Fatal(err)
	}
	teamBot(t, h, "stranger", []string{"other"}, nil)
	assignment := runOnNode(t, h, plane, "web-1", "ask-1")
	run := assignment.Run
	if run == nil || !contains(run.AllowedTools, DelegateToolName) {
		t.Fatalf("run = %+v", run)
	}
	if offer := run.Delegation; offer == nil || strings.Join(offer.Targets, ",") != "quad-1" {
		t.Fatalf("targets = %+v", run.Delegation)
	}
	prompt := run.Prompt.String()
	for _, want := range []string{"Your teammates (team port)", "- quad-1: You are quad-1. Tools: local_read. Runs on node quad-1. Takes delegated tasks.", "- quad-2: You are quad-2. Tools: local_read. Runs on any node.\n", "The task:\nFind out"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{"quad-3", "stranger"} {
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
	plane := teamBot(t, h, "plane", []string{"port"}, nil)
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	teamBot(t, h, "quad-2", []string{"port"}, func(d *BotDefinition) { d.Delegation = &domain.DelegationSnapshot{FromTeam: new(bool)} })
	teamBot(t, h, "stranger", []string{"other"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "delegation"}}
	})
	parent := runOnNode(t, h, plane, "web-1", "ask-1")
	answer := delegate(t, h, "web-1", parent.ExecutionID, "quad-1", "Return home")
	if answer.Refused != nil || !answer.Created {
		t.Fatalf("teammate: %+v", answer)
	}
	answer = delegate(t, h, "web-1", parent.ExecutionID, "quad-2", "Return home")
	if answer.Refused == nil || answer.Refused.Reason != domain.RefusedTargetUnavailable || !strings.Contains(answer.Refused.Message, "fromTeam false") {
		t.Errorf("refusing teammate: %+v", answer.Refused)
	}
	answer = delegate(t, h, "web-1", parent.ExecutionID, "stranger", "Return home")
	if answer.Refused == nil || answer.Refused.Reason != domain.RefusedNotAllowed || !strings.Contains(answer.Refused.Message, "it may ask: quad-1") {
		t.Errorf("stranger: %+v", answer.Refused)
	}
	answer = delegate(t, h, "web-1", parent.ExecutionID, "ghost", "Return home")
	if answer.Refused == nil || answer.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("ghost: %+v", answer.Refused)
	}
}

func TestDelegationFailsWhenTheBotsCannotBeListed(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	plane := teamBot(t, h, "plane", []string{"port"}, nil)
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	parent := runOnNode(t, h, plane, "web-1", "ask-1")
	h.bots.listFail = errBoom
	if _, err := NewDelegateTask(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, DelegateCommand{Bot: "ghost", Objective: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
	if _, err := NewSubmitTask(h.deps).Execute(context.Background(), SubmitTaskCommand{BotID: "plane", Objective: "again", IdempotencyKey: "ask-2"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewNextAssignment(h.deps).Execute(context.Background(), "web-1"); !errors.Is(err, errBoom) {
		t.Errorf("assignment err = %v", err)
	}
}

func TestListAndGetGroups(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	teamBot(t, h, "plane", []string{"port", "air"}, func(d *BotDefinition) {
		d.Mission = "Fly the coast at dawn. Then land."
		d.Delegation = &domain.DelegationSnapshot{FromTeam: new(bool)}
	})
	teamBot(t, h, "alone", nil, nil)
	teamBot(t, h, "other-port", []string{"port"}, func(d *BotDefinition) { d.Tenant = "harbour" })
	groups, err := NewListGroups(h.deps).Execute(ctx)
	if err != nil || len(groups) != 3 {
		t.Fatalf("groups = %+v %v", groups, err)
	}
	if groups[0].TenantID != "default" || groups[0].Name != "air" || groups[1].Name != "port" || groups[2].TenantID != "harbour" {
		t.Errorf("order = %+v", groups)
	}
	port := groups[1]
	if len(port.Members) != 2 || port.Members[0].BotID != "plane" || port.Members[1].BotID != "quad-1" {
		t.Fatalf("port = %+v", port)
	}
	plane := port.Members[0]
	if plane.Purpose != "Fly the coast at dawn." || plane.TakesDelegation || plane.Status != "Enabled" || plane.Where != "any node" || len(plane.Groups) != 2 {
		t.Errorf("plane = %+v", plane)
	}
	if !port.Members[1].TakesDelegation {
		t.Errorf("quad-1 = %+v", port.Members[1])
	}
	if got, err := NewGetGroup(h.deps).Execute(ctx, "air", ""); err != nil || got.Name != "air" {
		t.Errorf("get air = %+v %v", got, err)
	}
	if got, err := NewGetGroup(h.deps).Execute(ctx, "port", "harbour"); err != nil || len(got.Members) != 1 {
		t.Errorf("get harbour port = %+v %v", got, err)
	}
	var validation domain.ValidationError
	if _, err := NewGetGroup(h.deps).Execute(ctx, "port", ""); !errors.As(err, &validation) || !strings.Contains(validation.Reason, "default, harbour") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := NewGetGroup(h.deps).Execute(ctx, "sea", ""); !domain.IsNotFound(err) {
		t.Errorf("missing: %v", err)
	}
	if _, err := NewGetGroup(h.deps).Execute(ctx, "Sea", ""); err == nil {
		t.Error("a bad name was accepted")
	}
	if _, err := NewGetGroup(h.deps).Execute(ctx, "sea", "Bad Tenant"); err == nil {
		t.Error("a bad tenant was accepted")
	}
	h.bots.listFail = errBoom
	if _, err := NewGetGroup(h.deps).Execute(ctx, "port", ""); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
}

func TestRunTaskInProcessReadsTheDirectory(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	plane := teamBot(t, h, "plane", []string{"port"}, nil)
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	submitted, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: plane.ID().String(), Objective: "Look at the quay", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunTask(h.deps).Execute(ctx, submitted.Task.ID()); err != nil {
		t.Fatal(err)
	}
	request := h.runtime.requests[len(h.runtime.requests)-1]
	if !strings.Contains(request.Prompt.String(), "- quad-1: You are quad-1.") || request.Delegation != nil {
		t.Errorf("request = %+v", request)
	}
}
