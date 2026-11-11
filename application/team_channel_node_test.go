package application

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// isChannelOff reports the error of a node whose team channel is off.
func isChannelOff(err error) bool {
	return domain.IsForbidden(err) && strings.Contains(err.Error(), "team channel off")
}

// A bot that may ask other teams is offered neither the ask tool nor the
// status tool on a node with the team channel off, and that node may not
// ask nor read a question; switched on, the next run asks as before.
func TestANodeWithTheTeamChannelOffOffersNoAskTool(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	switchedNode(t, h, "qa-node", false, "qa")
	channelNode(t, h, "dev-node", "dev")
	asker := channelBot(t, h, "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}, nil, "qa-node")
	// Its installer lists the channel's tools in its own allowed tools.
	tools := []string{"local_read", AskToolName, QuestionStatusToolName}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, asker.ID(), BotPatch{AllowedTools: &tools}); err != nil {
		t.Fatal(err)
	}
	channelBot(t, h, "dev-a", []string{"dev"}, nil, []string{"qa"}, "dev-node")
	off := runOnNode(t, h, asker, "qa-node", "lead-1")
	if off.Run.Ask != nil || slices.Contains(off.Run.AllowedTools, AskToolName) || slices.Contains(off.Run.AllowedTools, QuestionStatusToolName) || slices.Contains(off.Run.NodeTools(), AskToolName) {
		t.Fatalf("offered on a node with the channel off: %+v %v %v", off.Run.Ask, off.Run.AllowedTools, off.Run.NodeTools())
	}
	if _, err := NewAskTeam(h.deps).Execute(ctx, "qa-node", off.ExecutionID, AskTeamCommand{Team: "dev", Question: "Who owns the importer?"}); !isChannelOff(err) {
		t.Fatalf("asked from a node with the channel off: %v", err)
	}
	if !hasAudit(h, "question.ask", domain.AuditDenied) {
		t.Error("the refused ask is not audited")
	}
	if event := h.log.last(domain.EventQuestionRefused); event.Type != "" {
		t.Errorf("a node's error is no refusal the model reads: %+v", event)
	}
	if event := h.log.last(domain.EventQuestionAsked); event.Type != "" {
		t.Errorf("a question was asked: %+v", event)
	}
	if _, err := NewGetQuestion(h.deps).Execute(ctx, "qa-node", off.ExecutionID, "task-any"); !isChannelOff(err) {
		t.Errorf("read a question from a node with the channel off: %v", err)
	}
	switchedNode(t, h, "qa-node", true, "qa")
	on := runOnNode(t, h, asker, "qa-node", "lead-2")
	if on.Run.Ask == nil || !slices.Contains(on.Run.AllowedTools, AskToolName) || !slices.Contains(on.Run.AllowedTools, QuestionStatusToolName) {
		t.Fatalf("not offered with the channel on: %+v %v", on.Run.Ask, on.Run.AllowedTools)
	}
	if answer := askTeam(t, h, "qa-node", on.ExecutionID, "dev", "Who owns the importer?"); answer.Refused != nil || answer.BotID != "dev-a" {
		t.Fatalf("answer = %+v %+v", answer, answer.Refused)
	}
}

// A member whose node shares the team's memory but has the team channel
// off is never picked, and such a node takes no question's task; switched
// on, it answers.
func TestAMemberOnANodeWithTheTeamChannelOffIsNeverPicked(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	channelNode(t, h, "qa-node", "qa")
	switchedNode(t, h, "dev-node", false, "dev")
	asker := channelBot(t, h, "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}, PerMinute: 10, MaxOpen: 8}, nil, "qa-node")
	channelBot(t, h, "dev-a", []string{"dev"}, nil, []string{"qa"}, "dev-node")
	parent := runOnNode(t, h, asker, "qa-node", "lead-1")
	if refused := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?").Refused; refused == nil || refused.Reason != domain.RefusedTeamUnavailable {
		t.Fatalf("picked on a node with the channel off: %+v", refused)
	}
	switchedNode(t, h, "dev-node", true, "dev")
	answer := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	if answer.Refused != nil || answer.BotID != "dev-a" {
		t.Fatalf("answer = %+v %+v", answer, answer.Refused)
	}
	// Switched off before it ran: the question waits and says why.
	switchedNode(t, h, "dev-node", false, "dev")
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, "dev-node"); err != nil || found {
		t.Fatalf("a node with the channel off took the question: %v %v", found, err)
	}
	if view := readQuestion(t, h, "qa-node", parent.ExecutionID, answer.TaskID); view.Waiting != "no Online node with the team channel on that shares team dev's memory" {
		t.Errorf("waiting = %q", view.Waiting)
	}
	switchedNode(t, h, "dev-node", true, "dev")
	if got := nextFor(t, h, "dev-node"); got.Run == nil || got.Run.TaskID != answer.TaskID {
		t.Fatalf("assignment = %+v", got)
	}
}

// A team's Jev watcher on a node with the team channel off is refused
// and audited, and touches nothing.
func TestAWatcherOnANodeWithTheTeamChannelOffIsRefused(t *testing.T) {
	h, parent, watcher := watching(t, "qa")
	ctx := context.Background()
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	h.clock.now = t0.Add(6 * time.Minute)
	r := registration("jev-node")
	r.Capabilities.Runtimes = []string{"jev"}
	if _, err := NewRegisterNode(h.deps).Execute(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWatchNext(h.deps).Execute(ctx, "jev-node", watcher.ExecutionID, "", false); !isChannelOff(err) {
		t.Errorf("watched on a node with the channel off: %v", err)
	}
	if _, err := NewDecideWatch(h.deps).Execute(ctx, "jev-node", watcher.ExecutionID, WatchDecision{Question: q.TaskID, Choice: "abandon"}); !isChannelOff(err) {
		t.Errorf("decided on a node with the channel off: %v", err)
	}
	if !hasAudit(h, "question.watch", domain.AuditDenied) {
		t.Error("the refusal is not audited")
	}
	if status := mustTask(t, h, q.TaskID).Status(); status != domain.TaskPending {
		t.Errorf("the question is %s", status)
	}
}

// A node that cannot be read is the route's error, not a refusal.
func TestAChannelRouteFromAnUnknownNodeFails(t *testing.T) {
	h := newHarness()
	if _, err := channelNodeOf(context.Background(), h.deps, "ghost", "ask a team"); !domain.IsNotFound(err) {
		t.Errorf("err = %v", err)
	}
}
