package application

import (
	"context"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestTheOperatorWritesToARunningTaskAndItsNodeReadsIt(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	running, err := h.deps.Executions.Get(ctx, execution)
	if err != nil {
		t.Fatal(err)
	}
	task := running.TaskID()

	sent, err := NewSendTaskMessage(h.deps).Execute(ctx, task, "  check row F too  ")
	if err != nil || sent.ExecutionID != execution || sent.TaskID != task {
		t.Fatalf("%+v %v", sent, err)
	}
	event := h.log.events[len(h.log.events)-1]
	if event.Type != domain.EventTaskMessage || event.ExecutionID != execution || event.Payload["text"] != "check row F too" {
		t.Fatalf("%+v", event)
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "task.message" || audit.Outcome != domain.AuditOK {
		t.Fatalf("%+v", audit)
	}
	if _, err := NewSendTaskMessage(h.deps).Execute(ctx, task, "and row G"); err != nil {
		t.Fatal(err)
	}

	read := NewExecutionMessages(h.deps)
	all, err := read.Execute(ctx, node.ID(), execution, "")
	if err != nil || len(all) != 2 || all[0].Text != "check row F too" || all[1].Text != "and row G" {
		t.Fatalf("%+v %v", all, err)
	}
	newer, err := read.Execute(ctx, node.ID(), execution, all[0].ID)
	if err != nil || len(newer) != 1 || newer[0].Text != "and row G" {
		t.Fatalf("%+v %v", newer, err)
	}
	if none, _ := read.Execute(ctx, node.ID(), execution, all[1].ID); len(none) != 0 {
		t.Fatalf("%+v", none)
	}
	if again, _ := read.Execute(ctx, node.ID(), execution, "evt-gone"); len(again) != 2 {
		t.Fatalf("a cursor out of the window reads them all: %+v", again)
	}
	if _, err := read.Execute(ctx, "db-1", execution, ""); !isState(err) {
		t.Fatalf("another node: %v", err)
	}
	if _, err := read.Execute(ctx, node.ID(), "exec-none", ""); err == nil {
		t.Fatal("an unknown execution is refused")
	}

	// Ended, the execution has nothing left to read and the task takes no
	// message.
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), execution, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}); err != nil {
		t.Fatal(err)
	}
	if after, err := read.Execute(ctx, node.ID(), execution, ""); err != nil || len(after) != 0 {
		t.Fatalf("%+v %v", after, err)
	}
	if _, err := NewSendTaskMessage(h.deps).Execute(ctx, task, "still there?"); !isState(err) {
		t.Fatalf("a finished task: %v", err)
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Outcome != domain.AuditDenied {
		t.Fatalf("%+v", audit)
	}
}

func TestAMessageNeedsWordsATaskAndARunOnANode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	pending := submit(t, h, bot, "k").Task
	if _, err := NewSendTaskMessage(h.deps).Execute(ctx, pending.ID(), "   "); err == nil {
		t.Fatal("an empty message is refused")
	}
	if _, err := NewSendTaskMessage(h.deps).Execute(ctx, "task-none", "hi"); !domain.IsNotFound(err) {
		t.Fatalf("%v", err)
	}
	if _, err := NewSendTaskMessage(h.deps).Execute(ctx, pending.ID(), "hi"); !isState(err) {
		t.Fatalf("a pending task: %v", err)
	}
}

func TestARunsReplyGoesToTheTaskTimeline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	record := NewRecordSaid(h.deps)
	if err := record.Execute(ctx, node.ID(), execution, SaidReply{InReplyTo: "check row F", Text: "  OK, row F next.  "}); err != nil {
		t.Fatal(err)
	}
	event := h.log.events[len(h.log.events)-1]
	if event.Type != domain.EventTaskSaid || event.ExecutionID != execution || event.Payload["text"] != "OK, row F next." || event.Payload["inReplyTo"] != "check row F" {
		t.Fatalf("%+v", event)
	}
	if err := record.Execute(ctx, node.ID(), execution, SaidReply{Text: strings.Repeat("a", MaxSaidLength+3)}); err != nil {
		t.Fatal(err)
	}
	if text := h.log.events[len(h.log.events)-1].Payload["text"].(string); len([]rune(text)) != MaxSaidLength {
		t.Fatalf("%d", len([]rune(text)))
	}
	if _, has := h.log.events[len(h.log.events)-1].Payload["inReplyTo"]; has {
		t.Fatal("a reply to nothing names nothing")
	}
	if err := record.Execute(ctx, node.ID(), execution, SaidReply{Text: "  "}); err == nil {
		t.Fatal("empty words are refused")
	}
	if err := record.Execute(ctx, "db-1", execution, SaidReply{Text: "x"}); !isState(err) {
		t.Fatalf("another node: %v", err)
	}
	if err := record.Execute(ctx, node.ID(), "exec-none", SaidReply{Text: "x"}); err == nil {
		t.Fatal("an unknown execution is refused")
	}
}
