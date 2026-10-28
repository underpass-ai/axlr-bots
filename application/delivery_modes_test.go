package application

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAWarningGoesIntoTheRunningTaskAsAMessage(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "warn.*"})
	busy := submitWith(t, h, "supervisor", "watch", "")
	running := nextFor(t, h, "web-1")
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.stock", Message: "low on boxes", Data: []byte(`{"left": 2}`)})
	if len(answer.Deliveries) != 1 {
		t.Fatalf("answer = %+v", answer)
	}
	d := answer.Deliveries[0]
	if d.Mode != "message" || d.TaskID != busy.ID().String() || d.MessageID == "" || d.Urgent {
		t.Errorf("delivery = %+v", d)
	}
	messages, err := NewExecutionMessages(h.deps).Execute(ctx, "web-1", running.ExecutionID, "")
	if err != nil || len(messages) != 1 || messages[0].ID != d.MessageID || messages[0].Text != "Your teammate recepcion says (warn.stock): low on boxes\n"+`{"left":2}` {
		t.Fatalf("messages = %+v %v", messages, err)
	}
	message, _ := lastEvent(h, domain.EventTaskMessage)
	if message.Payload["from"] != "event:"+answer.EventID || message.ExecutionID != running.ExecutionID {
		t.Errorf("message = %+v", message)
	}
	heard, _ := lastEvent(h, domain.EventEventHeard)
	if heard.Payload["mode"] != "message" || heard.Payload["messageId"] != d.MessageID || heard.BotID != "supervisor" {
		t.Errorf("heard = %+v", heard)
	}
	// The same call again gives nothing twice.
	again := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.stock", Message: "low on boxes", Data: []byte(`{"left": 2}`)})
	if !again.Deduplicated || len(again.Deliveries) != 1 || again.Deliveries[0].Created || again.Deliveries[0].MessageID != d.MessageID {
		t.Errorf("again = %+v", again)
	}
	if messages, _ := NewExecutionMessages(h.deps).Execute(ctx, "web-1", running.ExecutionID, ""); len(messages) != 1 {
		t.Errorf("a repeat sent %d messages", len(messages))
	}
}

func TestAWarningToAnIdleBotIsATask(t *testing.T) {
	h := newHarness()
	parent := shopTeam(t, h)
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "warn.*"})
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.stock", Message: "low on boxes"})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].Mode != "task" || answer.Deliveries[0].Urgent {
		t.Fatalf("answer = %+v", answer)
	}
	if heard, _ := lastEvent(h, domain.EventEventHeard); heard.Payload["mode"] != "task" {
		t.Errorf("heard = %+v", heard)
	}
	// An urgent subscription makes it an urgent task, whatever its mode.
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "warn.*", Mode: "note", Urgent: true})
	loud := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.fire-door", Message: "open"})
	if len(loud.Deliveries) != 1 || loud.Deliveries[0].Mode != "task" || !loud.Deliveries[0].Urgent {
		t.Errorf("urgent = %+v", loud)
	}
}

func TestNotesReachTheNextPrompt(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "recepcion", BotPatch{Events: &domain.EventsSnapshot{PerMinute: 20}}); err != nil {
		t.Fatal(err)
	}
	subscribe(t, h, "supervisor", domain.SubscriptionSnapshot{Topic: "info.*", Mode: "note"})
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.turno", Message: "shift change at six"})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].Mode != "note" || answer.Deliveries[0].TaskID != "" {
		t.Fatalf("answer = %+v", answer)
	}
	if heard, _ := lastEvent(h, domain.EventEventHeard); heard.Payload["mode"] != "note" || heard.BotID != "supervisor" {
		t.Errorf("heard = %+v", heard)
	}
	// An addressed info is a note too: no task.
	addressed := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.x", Message: "for you", To: []string{"almacen"}})
	if len(addressed.Deliveries) != 1 || addressed.Deliveries[0].Mode != "note" {
		t.Errorf("addressed = %+v", addressed)
	}
	// Nine notes in a minute: the prompt carries the newest eight.
	for i := 2; i <= 9; i++ {
		h.clock.now = h.clock.now.Add(time.Second)
		emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.n" + fmt.Sprint(i), Message: fmt.Sprintf("note %d", i)})
	}
	report(t, h, parent)
	task := submitWith(t, h, "supervisor", "next", "")
	next := nextFor(t, h, "web-1")
	prompt := next.Run.Prompt.String()
	if !strings.Contains(prompt, "Recent events in your team") || strings.Contains(prompt, "shift change at six") || !strings.Contains(prompt, "recepcion (info.n9): note 9") || !strings.Contains(prompt, "recepcion (info.n2): note 2") {
		t.Errorf("prompt = %s", prompt)
	}
	if !strings.Contains(prompt, "Your mission, set by your operator") || !strings.HasSuffix(prompt, "The task:\nDo next") {
		t.Errorf("mission or objective cut: %s", prompt)
	}
	kept := taskOf(t, h, task.ID())
	if notes := kept.Notes().List(); len(notes) != 8 || notes[7].Name != "info.n9" || !kept.HasNotes() {
		t.Errorf("kept = %+v", notes)
	}
	if got, _ := h.tasks.Get(ctx, task.ID()); !strings.Contains(string(got.Input()), `"notes"`) {
		t.Errorf("input = %s", got.Input())
	}
}

func TestAPausedBotReadsItsNotesWhenEnabled(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := shopTeam(t, h)
	subscribe(t, h, "caja", domain.SubscriptionSnapshot{Topic: "info.*"})
	if _, err := NewPauseBot(h.deps).Execute(ctx, "caja"); err != nil {
		t.Fatal(err)
	}
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.precios", Message: "new prices tomorrow"})
	if len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "caja" || answer.Deliveries[0].Mode != "note" {
		t.Fatalf("answer = %+v", answer)
	}
	// A Paused bot takes no message and no task.
	warned := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.x", Message: "x", To: []string{"caja"}})
	if len(warned.Skipped) != 1 || warned.Skipped[0].Reason != domain.RefusedTargetUnavailable {
		t.Errorf("warned = %+v", warned)
	}
	report(t, h, parent)
	if _, err := NewEnableBot(h.deps).Execute(ctx, "caja"); err != nil {
		t.Fatal(err)
	}
	submitWith(t, h, "caja", "open", "")
	if prompt := nextFor(t, h, "web-1").Run.Prompt.String(); !strings.Contains(prompt, "new prices tomorrow") {
		t.Errorf("prompt = %s", prompt)
	}
}

// report ends the assignment's execution.
func report(t *testing.T, h *harness, a Assignment) {
	t.Helper()
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", a.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}); err != nil {
		t.Fatal(err)
	}
}
