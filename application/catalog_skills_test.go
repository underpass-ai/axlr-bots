package application

import (
	"context"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func oomPin(version string) []domain.SkillPinSnapshot {
	return []domain.SkillPinSnapshot{{Name: "oom", Version: version, Catalog: "axlr"}}
}

func skilledBot(t *testing.T, h *harness, placement domain.PlacementSnapshot) domain.Bot {
	t.Helper()
	d := sampleDefinition()
	d.Skills = oomPin("1.0.0")
	return placedBot(t, h, d, placement)
}

// skilledNode registers a node whose axlr-tui reads pinned skills.
func skilledNode(t *testing.T, h *harness, id string) domain.Node {
	t.Helper()
	r := registration(id)
	r.Capabilities.SkillsSupported = true
	node, err := NewRegisterNode(h.deps).Execute(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestRegisterAndUpdateValidateABotsSkills(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := sampleDefinition()
	d.Skills = oomPin("latest")
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil || !strings.Contains(err.Error(), "exact major.minor.patch") {
		t.Errorf("register latest: %v", err)
	}
	d = sampleDefinition()
	d.Skills = oomPin("1.0.0")
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if got := bot.Skills().All(); len(got) != 1 || got[0].Key() != "axlr/oom@1.0.0" {
		t.Fatalf("skills = %+v", got)
	}
	ranges := oomPin("^1.0.0")
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &ranges}); err == nil {
		t.Error("update took a range")
	}
	stored, _ := h.bots.Get(ctx, bot.ID())
	if stored.Skills().All()[0].Version() != "1.0.0" {
		t.Errorf("a refused patch changed the bot: %+v", stored.Skills())
	}
	next := oomPin("1.1.0")
	updated, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &next})
	if err != nil || updated.Skills().All()[0].Version() != "1.1.0" {
		t.Fatalf("update: %v %+v", err, updated.Skills())
	}
	none := []domain.SkillPinSnapshot{}
	cleared, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &none})
	if err != nil || !cleared.Skills().IsEmpty() {
		t.Errorf("clear: %v %+v", err, cleared.Skills())
	}
	if got := h.log.audits[len(h.log.audits)-1]; got.Action != "bot.update" || got.Details["fields"] != "skills" {
		t.Errorf("audit = %+v", got)
	}
}

func TestAJevBotTakesNoSkills(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := jevDefinition()
	d.Skills = oomPin("1.0.0")
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil || !strings.Contains(err.Error(), "no language model") {
		t.Errorf("register: %v", err)
	}
	d.Skills = nil
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	pins := oomPin("1.0.0")
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &pins}); err == nil || !strings.Contains(err.Error(), "no language model") {
		t.Errorf("update: %v", err)
	}
	// One patch that leaves Jev and adds the skills holds.
	axlr := &domain.RuntimeSnapshot{Kind: "axlr"}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Runtime: axlr, Skills: &pins}); err != nil {
		t.Errorf("to axlr with skills: %v", err)
	}
}

// A task runs the versions its bot had when it was submitted, in process
// and on a node, whatever the bot says later.
func TestATaskIsPinnedAtSubmission(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := skilledBot(t, h, domain.PlacementSnapshot{})
	task := submit(t, h, bot, "k-1").Task
	next := oomPin("2.0.0")
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &next}); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.tasks.Get(ctx, task.ID())
	if got := stored.Snapshot().Skills; len(got) != 1 || got[0].Version != "1.0.0" {
		t.Fatalf("task skills = %+v", got)
	}
	if _, err := NewRunTask(h.deps).Execute(ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	if got := h.runtime.requests[0].Skills.All(); len(got) != 1 || got[0].Version() != "1.0.0" {
		t.Errorf("in process = %+v", got)
	}
}

func TestOnlyANodeThatReadsSkillsTakesATaskWithThem(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := skilledBot(t, h, domain.PlacementSnapshot{Nodes: []string{"web-1"}})
	onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-1").Task
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err != nil || found {
		t.Fatalf("an old node took a task with skills: %v %v", err, found)
	}
	if got := waitOf(t, h, task.ID()); !strings.Contains(got, "takes no pinned skills") {
		t.Errorf("wait = %q", got)
	}
	skilledNode(t, h, "web-1")
	if got := waitOf(t, h, task.ID()); got != "" {
		t.Errorf("wait with a capable node = %q", got)
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1")
	if err != nil || !found || assignment.Run == nil {
		t.Fatalf("assignment: %v %v", err, found)
	}
	if got := assignment.Run.Skills.All(); len(got) != 1 || got[0].Key() != "axlr/oom@1.0.0" {
		t.Errorf("assignment skills = %+v", got)
	}
}

// The task's own pins decide, even once its bot has none: a placed task
// waits for a node that can read them and says so.
func TestAPlacedTaskWithSkillsWaitsAfterItsBotDropsThem(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := skilledBot(t, h, domain.PlacementSnapshot{Nodes: []string{"web-1"}})
	onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-1").Task
	none := []domain.SkillPinSnapshot{}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &none}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err != nil || found {
		t.Errorf("an old node took a task with skills: %v %v", err, found)
	}
	if got := waitOf(t, h, task.ID()); !strings.Contains(got, "takes no pinned skills") {
		t.Errorf("wait = %q", got)
	}
	skilledNode(t, h, "web-1")
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1")
	if err != nil || !found || assignment.Run.Skills.All()[0].Version() != "1.0.0" {
		t.Errorf("a node that reads skills: %v %v %+v", err, found, assignment.Run)
	}
}

// An unplaced one runs in process, where the run still reads its skills.
func TestAnUnplacedTaskWithSkillsRunsInProcessAfterItsBotDropsThem(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := skilledBot(t, h, domain.PlacementSnapshot{})
	onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-1").Task
	none := []domain.SkillPinSnapshot{}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &none}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err != nil || found {
		t.Errorf("an old node took a task with skills: %v %v", err, found)
	}
	if _, ran, err := NewRunNextPending(h.deps).Execute(ctx); err != nil || !ran {
		t.Fatalf("not run in process: %v %v", ran, err)
	}
	if got := h.runtime.requests[0]; got.TaskID != task.ID() || got.Skills.All()[0].Version() != "1.0.0" {
		t.Errorf("request = %+v", got)
	}
}

// A task without pins asks nothing of a node, whatever its bot gained.
func TestATaskWithoutSkillsStillRunsOnAnOldNodeAfterItsBotGainsThem(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := placedBot(t, h, sampleDefinition(), domain.PlacementSnapshot{Nodes: []string{"web-1"}})
	onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-1").Task
	pins := oomPin("1.0.0")
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Skills: &pins}); err != nil {
		t.Fatal(err)
	}
	if got := waitOf(t, h, task.ID()); got != "" {
		t.Errorf("wait = %q", got)
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1")
	if err != nil || !found || assignment.Run.TaskID != task.ID() || !assignment.Run.Skills.IsEmpty() {
		t.Errorf("assignment: %v %v %+v", err, found, assignment.Run)
	}
}

func TestABotWithoutSkillsGoesToAnyNodeAndCarriesNone(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k-1").Task
	if !task.Skills().IsEmpty() {
		t.Errorf("task skills = %+v", task.Skills())
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1")
	if err != nil || !found || !assignment.Run.Skills.IsEmpty() {
		t.Errorf("assignment: %v %v %+v", err, found, assignment.Run)
	}
}

// An unplaced bot's task runs in process while no Online node can read
// its skills, and never goes to the one that cannot.
func TestAnUnplacedBotWithSkillsRunsInProcessBesideAnOldNode(t *testing.T) {
	h := newHarness()
	bot := skilledBot(t, h, domain.PlacementSnapshot{})
	onlineNode(t, h, "web-1")
	submit(t, h, bot, "k-1")
	if !picked(t, h) {
		t.Error("the only Online node cannot read the skills, and the task stays with nobody")
	}
}

// A decision continues under the skills of its task.
func TestADecisionCarriesTheTasksSkills(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := skilledBot(t, h, domain.PlacementSnapshot{})
	task := submit(t, h, bot, "k-1").Task
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "s", PendingDecision: &domain.PendingDecision{SessionID: "s", CallID: "c", Tool: "local_exec"}}}
	if _, err := NewRunTask(h.deps).Execute(ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, "c", "tirso"); err != nil {
		t.Fatal(err)
	}
	if got := h.decider.requests[0].Skills.All(); len(got) != 1 || got[0].Version() != "1.0.0" {
		t.Errorf("decision skills = %+v", got)
	}
}
