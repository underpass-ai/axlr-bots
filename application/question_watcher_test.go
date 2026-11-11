package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// watcherRuntime is a team's Jev watcher's program.
func watcherRuntime() *domain.RuntimeSnapshot {
	return &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
		Server: domain.WatchServer, Start: domain.WatchNextTool.String(), Wait: domain.WatchNextTool.String(),
		Act: domain.JevActSnapshot{Tool: domain.WatchDecideTool.String(), Choice: domain.WatchChoice, Verdict: domain.WatchVerdict},
	}}
}

// watching is the channel (qa-jefe asks dev) with a Jev node, and a Jev
// watcher of the team named running there: its execution.
func watching(t *testing.T, team string) (*harness, Assignment, Assignment) {
	t.Helper()
	h, parent := channel(t)
	r := registration("jev-node")
	r.Capabilities.Runtimes = []string{"jev"}
	if _, err := NewRegisterNode(h.deps).Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	watcher := watcherBot(t, h, team+"-vigia", team, watcherRuntime())
	return h, parent, runOnNode(t, h, watcher, "jev-node", "watch-"+team)
}

func watcherBot(t *testing.T, h *harness, id, team string, runtime *domain.RuntimeSnapshot) domain.Bot {
	t.Helper()
	d := sampleDefinition()
	d.ID, d.Name, d.Tenant = id, id, "default"
	d.AllowedTools = []string{domain.WatchNextTool.String(), domain.WatchDecideTool.String()}
	d.Level, d.Runtime = "act-within-policy", runtime
	if team != "" {
		d.Groups = []string{team}
	}
	d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}}
	d.Placement = domain.PlacementSnapshot{Nodes: []string{"jev-node"}}
	bot, err := NewRegisterBot(h.deps).Execute(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(context.Background(), bot.ID()); err != nil {
		t.Fatal(err)
	}
	return bot
}

func watchNext(t *testing.T, h *harness, execution domain.ExecutionID, after domain.TaskID) WatchedQuestion {
	t.Helper()
	watched, err := NewWatchNext(h.deps).Execute(context.Background(), "jev-node", execution, after)
	if err != nil {
		t.Fatal(err)
	}
	return watched
}

func hasAudit(h *harness, action string, outcome domain.AuditOutcome) bool {
	for _, entry := range h.log.audits {
		if entry.Action == action && entry.Outcome == outcome {
			return true
		}
	}
	return false
}

// Only a Jev watcher watches: an AXLR bot, or a Jev bot of another
// server, is refused and audited; a node that does not hold the
// execution is refused too.
func TestOnlyAJevWatcherWatchesQuestions(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	ctx := context.Background()
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(6 * time.Minute)
	if _, err := NewWatchNext(h.deps).Execute(ctx, "qa-node", parent.ExecutionID, ""); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "is not a Jev watcher") {
		t.Errorf("an AXLR bot watched: %v", err)
	}
	if _, err := NewDecideWatch(h.deps).Execute(ctx, "qa-node", parent.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "abandon"}); !domain.IsForbidden(err) {
		t.Errorf("an AXLR bot decided: %v", err)
	}
	if !hasAudit(h, "question.watch", domain.AuditDenied) {
		t.Error("the refusal is not audited")
	}
	if _, err := NewWatchNext(h.deps).Execute(ctx, "qa-node", watcher.ExecutionID, ""); err == nil {
		t.Error("another node read the watcher's round")
	}
	other := &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{Server: "queue", Start: domain.WatchNextTool.String(), Wait: domain.WatchNextTool.String(), Act: domain.JevActSnapshot{Tool: domain.WatchDecideTool.String(), Choice: "choice"}}}
	worker := watcherBot(t, h, "qa-worker", "qa", other)
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: worker.ID().String(), Objective: "Work", IdempotencyKey: "work-1"}); err != nil {
		t.Fatal(err)
	}
	// The watcher's own run holds jev-node's bot, not the node: the worker
	// gets its execution too.
	run := nextFor(t, h, "jev-node")
	if _, err := NewWatchNext(h.deps).Execute(ctx, "jev-node", run.ExecutionID, ""); !domain.IsForbidden(err) {
		t.Errorf("a Jev bot of another server watched: %v", err)
	}
	if status := mustTask(t, h, q.TaskID).Status(); status != domain.TaskPending {
		t.Errorf("the question is %s", status)
	}
}

// A watcher with no team has nothing to watch.
func TestAWatcherInNoTeamIsRefused(t *testing.T) {
	h, _, _ := watching(t, "qa")
	lone := watcherBot(t, h, "lone-vigia", "", watcherRuntime())
	run := runOnNode(t, h, lone, "jev-node", "lone-1")
	if _, err := NewWatchNext(h.deps).Execute(context.Background(), "jev-node", run.ExecutionID, ""); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "in no team") {
		t.Errorf("a watcher in no team: %v", err)
	}
}
