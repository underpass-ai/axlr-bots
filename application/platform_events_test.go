package application

import (
	"context"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func fail(t *testing.T, h *harness, a Assignment, reason string) {
	t.Helper()
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", a.ExecutionID, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: reason}); err != nil {
		t.Fatal(err)
	}
}

func emittedBy(h *harness, bot string) []domain.Event {
	var out []domain.Event
	for _, e := range h.log.events {
		if e.Type == domain.EventEventEmitted && e.BotID.String() == bot {
			out = append(out, e)
		}
	}
	return out
}

func TestAFailureReachesWhoListensToErrors(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "error.*", Mode: "task"})
	fail(t, h, parent, "the printer is out of paper")
	emitted := emittedBy(h, "recepcion")
	if len(emitted) != 1 || emitted[0].Payload["by"] != "control-plane" || emitted[0].Payload["name"] != "error.bot" || emitted[0].Payload["severity"] != "critical" {
		t.Fatalf("emitted = %+v", emitted)
	}
	d := emitted[0].Payload["deliveries"].([]map[string]any)
	if len(d) != 1 || d[0]["botId"] != "supervisor" || d[0]["urgent"] != true {
		t.Fatalf("deliveries = %+v", d)
	}
	task := taskOf(t, h, domain.TaskID(d[0]["taskId"].(string)))
	objective := task.Objective().String()
	if !task.IsUrgent() || !strings.Contains(objective, "recepcion") || !strings.Contains(objective, "error.bot") || !strings.Contains(objective, "the printer is out of paper") {
		t.Errorf("task = %v %q", task.IsUrgent(), objective)
	}
	if heard, _ := lastEvent(h, domain.EventEventHeard); heard.Payload["by"] != "control-plane" || heard.Payload["from"] != "recepcion" {
		t.Errorf("heard = %+v", heard)
	}
}

func TestNobodyListensNothingIsPublished(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	fail(t, h, parent, "boom")
	if emitted := emittedBy(h, "recepcion"); len(emitted) != 0 {
		t.Errorf("emitted = %+v", emitted)
	}
	if failed, ok := lastEvent(h, domain.EventTaskFailed); !ok || failed.BotID != "recepcion" {
		t.Errorf("failed = %+v", failed)
	}
}

func TestACompletedTaskIsANoteForWhoListens(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	subscribe(t, h, "almacen", domain.SubscriptionSnapshot{Topic: "done.*"})
	subscribe(t, h, "recepcion", domain.SubscriptionSnapshot{Topic: "done.*"})
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: strings.Repeat("a", 300)}}); err != nil {
		t.Fatal(err)
	}
	emitted := emittedBy(h, "recepcion")
	if len(emitted) != 1 || emitted[0].Payload["name"] != "done.task" {
		t.Fatalf("emitted = %+v", emitted)
	}
	if answer := emitted[0].Payload["data"].(map[string]any)["answer"].(string); len(answer) != 280 {
		t.Errorf("answer is %d long", len(answer))
	}
	d := emitted[0].Payload["deliveries"].([]map[string]any)
	if len(d) != 1 || d[0]["botId"] != "almacen" || d[0]["mode"] != "note" {
		t.Errorf("deliveries = %+v (recepcion never hears its own)", d)
	}
	submitWith(t, h, "almacen", "count", "")
	if prompt := nextFor(t, h, "web-1").Run.Prompt.String(); !strings.Contains(prompt, "recepcion (done.task): task "+parent.Run.TaskID.String()+" completed") {
		t.Errorf("prompt = %s", prompt)
	}
}

func TestAWaitingTaskIsAMessageForARunningSupervisor(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "warn.approval", Mode: "message"})
	submitWith(t, h, "supervisor", "watch", "")
	watching := nextFor(t, h, "web-1")
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, pendingCall("local_write", `{"path": "x"}`)); err != nil {
		t.Fatal(err)
	}
	messages, err := NewExecutionMessages(h.deps).Execute(ctx, "web-1", watching.ExecutionID, "")
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Text, "(warn.approval): task "+parent.Run.TaskID.String()+" waits for a person to approve local_write") {
		t.Errorf("messages = %+v %v", messages, err)
	}
}

func TestAChainOfFailuresIsCut(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	for _, bot := range []string{"supervisor", "almacen", "caja"} {
		subscribe(t, h, bot, domain.SubscriptionSnapshot{Topic: "error.*", Mode: "task", From: []string{map[string]string{"supervisor": "recepcion", "almacen": "supervisor", "caja": "almacen"}[bot]}})
	}
	fail(t, h, parent, "first")
	first := nextFor(t, h, "web-1")
	if first.Run.BotID != "supervisor" {
		t.Fatalf("first = %s", first.Run.BotID)
	}
	fail(t, h, first, "second")
	second := nextFor(t, h, "web-1")
	if second.Run.BotID != "almacen" {
		t.Fatalf("second = %s", second.Run.BotID)
	}
	if lineage, _ := taskOf(t, h, second.Run.TaskID).Lineage(); lineage.Depth != 2 {
		t.Errorf("depth = %d", lineage.Depth)
	}
	fail(t, h, second, "third")
	emitted := emittedBy(h, "almacen")
	if len(emitted) != 1 {
		t.Fatalf("emitted = %+v", emitted)
	}
	skipped := emitted[0].Payload["skipped"].([]map[string]any)
	if len(skipped) != 1 || skipped[0]["botId"] != "caja" || skipped[0]["reason"] != domain.RefusedDepthExceeded || len(emitted[0].Payload["deliveries"].([]map[string]any)) != 0 {
		t.Errorf("third = %+v", emitted[0].Payload)
	}
	if _, found, _ := NewNextAssignment(h.deps).Execute(context.Background(), "web-1"); found {
		t.Error("a fourth task was made")
	}
}
