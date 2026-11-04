package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestARunsProgressGoesToTheTaskTimeline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	record := NewRecordProgress(h.deps)
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	batch := ProgressBatch{Items: []ProgressItem{
		{Kind: ProgressToolStarted, At: at, Tool: "axlr_call_tool", MCPTool: "list_pods", CallID: "c1", Summary: strings.Repeat("s", 400), Text: "never kept"},
		{Kind: ProgressToolEnded, At: at, Tool: "axlr_call_tool", CallID: "c1", Status: "ok", DurationMS: 120, Summary: "never kept"},
		{Kind: ProgressText, Text: "Looking at the pods."},
		{Kind: ProgressDelegation, Bot: "worker", TaskID: "task-child"},
		{Kind: ProgressDecisionRequired, Tool: "local_exec", CallID: "c2"},
		{Kind: ProgressTextTruncated},
		{Kind: "tool.exploded", Tool: "x"},
		{Kind: ProgressText, Text: "   "},
	}, Dropped: 3}
	answer, err := record.Execute(ctx, node.ID(), execution, batch)
	if err != nil || !answer.Recorded || answer.Full {
		t.Fatalf("%+v %v", answer, err)
	}
	event := h.log.events[len(h.log.events)-1]
	if event.Type != domain.EventTaskProgress || event.ExecutionID != execution || event.Payload["dropped"] != 3 {
		t.Fatalf("%+v", event)
	}
	items := event.Payload["items"].([]ProgressItem)
	if len(items) != 6 {
		t.Fatalf("an unknown kind and empty words are left out: %+v", items)
	}
	if started := items[0]; started.MCPTool != "list_pods" || started.Text != "" || len([]rune(started.Summary)) != maxProgressSummary || !strings.HasSuffix(started.Summary, "…") {
		t.Fatalf("a call keeps its own fields, cut: %+v", started)
	}
	if ended := items[1]; ended.Summary != "" || ended.DurationMS != 120 || ended.Status != "ok" {
		t.Fatalf("%+v", ended)
	}
	if text := items[2]; text.At.IsZero() || text.Text != "Looking at the pods." {
		t.Fatalf("an item without a time takes the control plane's: %+v", text)
	}
	if items[3].Bot != "worker" || items[3].TaskID != "task-child" || items[4].Tool != "local_exec" {
		t.Fatalf("%+v", items)
	}

	if _, err := record.Execute(ctx, node.ID(), execution, ProgressBatch{Items: []ProgressItem{{Kind: "nothing"}}}); err == nil {
		t.Fatal("a batch with nothing known is refused")
	}
	if _, err := record.Execute(ctx, node.ID(), execution, ProgressBatch{Items: make([]ProgressItem, MaxProgressItems+1)}); err == nil {
		t.Fatal("a batch past its bound is refused")
	}
	one := ProgressBatch{Items: []ProgressItem{{Kind: ProgressText, Text: "x"}}}
	if _, err := record.Execute(ctx, "db-1", execution, one); !isState(err) {
		t.Fatalf("another node: %v", err)
	}
	if _, err := record.Execute(ctx, node.ID(), "exec-none", one); err == nil {
		t.Fatal("an unknown execution is refused")
	}
	h.log.fail = errBoom
	if _, err := record.Execute(ctx, node.ID(), execution, one); !errors.Is(err, errBoom) {
		t.Fatalf("a timeline that cannot be read: %v", err)
	}
	h.log.fail = nil

	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), execution, ExecutionReport{Outcome: domain.ExecutionCompleted}); err != nil {
		t.Fatal(err)
	}
	if _, err := record.Execute(ctx, node.ID(), execution, one); !isState(err) {
		t.Fatalf("an ended execution: %v", err)
	}
}

func TestAnExecutionsProgressIsBounded(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	record := NewRecordProgress(h.deps)
	one := ProgressBatch{Items: []ProgressItem{{Kind: ProgressText, Text: "x"}}}
	for i := 0; i < MaxProgressEvents-1; i++ {
		if answer, err := record.Execute(ctx, node.ID(), execution, one); err != nil || !answer.Recorded || answer.Full {
			t.Fatalf("%d: %+v %v", i, answer, err)
		}
	}
	// The last event the bound allows says it is the last.
	answer, err := record.Execute(ctx, node.ID(), execution, one)
	if err != nil || !answer.Recorded || !answer.Full || h.log.events[len(h.log.events)-1].Payload["truncated"] != true {
		t.Fatalf("%+v %v", answer, err)
	}
	before := len(h.log.events)
	if answer, err := record.Execute(ctx, node.ID(), execution, one); err != nil || answer.Recorded || !answer.Full || len(h.log.events) != before {
		t.Fatalf("past the bound nothing more: %+v %v", answer, err)
	}
}

func TestAnExecutionsProgressIsBoundedInBytes(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	record := NewRecordProgress(h.deps)
	words := strings.Repeat("w", MaxProgressText)
	big := ProgressBatch{}
	for range MaxProgressItems {
		big.Items = append(big.Items, ProgressItem{Kind: ProgressText, Text: words})
	}
	if answer, err := record.Execute(ctx, node.ID(), execution, big); err != nil || !answer.Recorded || answer.Full {
		t.Fatalf("%+v %v", answer, err)
	}
	answer, err := record.Execute(ctx, node.ID(), execution, big)
	event := h.log.events[len(h.log.events)-1]
	if err != nil || !answer.Full || event.Payload["truncated"] != true || len(event.Payload["items"].([]ProgressItem)) >= MaxProgressItems {
		t.Fatalf("what fits is kept and the rest cut: %+v %v", answer, err)
	}
	if answer, _ := record.Execute(ctx, node.ID(), execution, big); answer.Recorded || !answer.Full {
		t.Fatalf("%+v", answer)
	}
}

func TestATasksTimelineReadsAfterTheCallersLastEvent(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	running, err := h.deps.Executions.Get(ctx, execution)
	if err != nil {
		t.Fatal(err)
	}
	task := running.TaskID()
	if err := NewRecordSaid(h.deps).Execute(ctx, node.ID(), execution, SaidReply{Text: "first"}); err != nil {
		t.Fatal(err)
	}
	all, err := NewListTaskEvents(h.deps).Query(ctx, TaskEventsQuery{ID: task})
	if err != nil || len(all) == 0 {
		t.Fatalf("%+v %v", all, err)
	}
	last := all[len(all)-1].ID.String()
	if none, err := NewListTaskEvents(h.deps).Query(ctx, TaskEventsQuery{ID: task, After: last}); err != nil || len(none) != 0 {
		t.Fatalf("%+v %v", none, err)
	}
	if err := NewRecordSaid(h.deps).Execute(ctx, node.ID(), execution, SaidReply{Text: "second"}); err != nil {
		t.Fatal(err)
	}
	newer, err := NewListTaskEvents(h.deps).Query(ctx, TaskEventsQuery{ID: task, After: last})
	if err != nil || len(newer) != 1 || newer[0].Payload["text"] != "second" {
		t.Fatalf("%+v %v", newer, err)
	}
	if again, _ := NewListTaskEvents(h.deps).Query(ctx, TaskEventsQuery{ID: task, After: "evt-gone"}); len(again) != len(all)+1 {
		t.Fatalf("a cursor out of the window reads them all: %d", len(again))
	}
}

// A failed call's error reaches the timeline cut to its bound, and only
// on the item that says how the call ended.
func TestAFailedCallsErrorIsKeptShort(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	ended, ok := ProgressItem{Kind: ProgressToolEnded, Tool: "axlr_call_tool", CallID: "c1", Status: "failed", Error: strings.Repeat("é", 300)}.normalized(now)
	if !ok || ended.Status != "failed" || len([]rune(ended.Error)) != maxProgressError || !strings.HasSuffix(ended.Error, "…") {
		t.Fatalf("%+v", ended)
	}
	started, _ := ProgressItem{Kind: ProgressToolStarted, Tool: "axlr_call_tool", Error: "not here"}.normalized(now)
	if started.Error != "" {
		t.Fatalf("%+v", started)
	}
}
