package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The asker still runs: its message carries what qa's store keeps of the
// question, and the responder's report answer what dev's keeps.
func TestBothSidesRecordsTravelWithTheAnswer(t *testing.T) {
	h, parent := channel(t)
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	got := nextFor(t, h, "dev-node")
	if got.Run.ChannelMemory != nil {
		t.Fatalf("the question's run carries a record: %+v", got.Run.ChannelMemory)
	}
	report := ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "Alice owns it."}}
	result, err := NewReportExecution(h.deps).Execute(context.Background(), "dev-node", got.ExecutionID, report)
	if err != nil {
		t.Fatal(err)
	}
	answered := domain.ChannelRecord{
		Side: domain.ChannelAnswered, Team: "dev", Other: "qa", Question: q.TaskID, Asker: "qa-jefe", Responder: "dev-a",
		Text: "Who owns the importer?", Answered: true, Answer: "Alice owns it.", Status: domain.TaskCompleted,
	}
	if result.ChannelMemory == nil || *result.ChannelMemory != answered {
		t.Fatalf("report memory = %+v", result.ChannelMemory)
	}
	if audit, _ := lastAudit(h, "question.memory"); audit.Outcome != domain.AuditOK || audit.Details["side"] != "answered" || audit.Details["mode"] != "report" || audit.Details["node"] != "dev-node" || audit.Details["about"] != "project:dev.canal" {
		t.Errorf("audit = %+v", audit)
	}
	// A repeated report (its first answer lost) carries it again, and is
	// not audited twice.
	audits := len(h.log.audits)
	again, err := NewReportExecution(h.deps).Execute(context.Background(), "dev-node", got.ExecutionID, report)
	if err != nil || again.Accepted || again.ChannelMemory == nil || *again.ChannelMemory != answered {
		t.Fatalf("repeat = %+v %v", again, err)
	}
	if len(h.log.audits) != audits {
		t.Errorf("a repeat audited: %+v", h.log.audits[audits:])
	}
	messages, err := NewExecutionMessages(h.deps).Execute(context.Background(), "qa-node", parent.ExecutionID, "")
	if err != nil || len(messages) != 1 || messages[0].Memory == nil {
		t.Fatalf("messages: %v %+v", err, messages)
	}
	asked := answered
	asked.Side, asked.Team, asked.Other = domain.ChannelAsked, "qa", "dev"
	if *messages[0].Memory != asked.Snapshot() {
		t.Errorf("message memory = %+v", messages[0].Memory)
	}
	if audit := auditOf(h, "question.memory", "asked"); audit.Outcome != domain.AuditOK || audit.Details["mode"] != "message" || audit.Details["team"] != "qa" {
		t.Errorf("audit = %+v", audit)
	}
}

// The asker ended: the answer's task carries the asking side's record in
// its input, and its run hands it to the node.
func TestTheAnswersTaskCarriesTheAskingSidesRecord(t *testing.T) {
	h, parent := channel(t)
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "qa-node", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "asked dev"}}); err != nil {
		t.Fatal(err)
	}
	answerOn(t, h, q.TaskID, ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: "the model gave up"})
	got := nextFor(t, h, "qa-node")
	memory := got.Run.ChannelMemory
	if memory == nil || memory.Side != domain.ChannelAsked || memory.Team != "qa" || memory.Other != "dev" || memory.Answered || memory.Status != domain.TaskFailed || memory.Question != q.TaskID {
		t.Fatalf("run memory = %+v", memory)
	}
	var input map[string]any
	if err := json.Unmarshal(mustTask(t, h, got.Run.TaskID).Input(), &input); err != nil || input["memory"] == nil || input["answered"] != false {
		t.Errorf("input: %v %+v", err, input)
	}
	if audit := auditOf(h, "question.memory", "asked"); audit.Details["mode"] != "task" {
		t.Errorf("audit = %+v", audit)
	}
	// The bot.question.answered event keeps its facts, not the record.
	for _, event := range answeredEvents(h, q.TaskID) {
		if event.Payload["memory"] != nil {
			t.Errorf("event carries the record: %+v", event.Payload)
		}
	}
}

// A task that is no question carries no record anywhere.
func TestOnlyAQuestionHasARecord(t *testing.T) {
	h, parent := channel(t)
	result, err := NewReportExecution(h.deps).Execute(context.Background(), "qa-node", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}})
	if err != nil || result.ChannelMemory != nil {
		t.Fatalf("%+v %v", result.ChannelMemory, err)
	}
	task := mustTask(t, h, parent.Run.TaskID)
	if answerMemory(task) != nil {
		t.Error("a run task's input read as a record")
	}
	if _, err := (channelExchange{question: task}).record(domain.ChannelAsked); err == nil {
		t.Error("a record of a task that is no question")
	}
}

// A question whose input lost its text makes no record: the answer still
// goes back, and the audit says why.
func TestARecordThatCannotBeMadeIsAuditedNotDelivered(t *testing.T) {
	h, parent := channel(t)
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	responder, _ := h.bots.Get(context.Background(), "dev-a")
	child := mustTask(t, h, q.TaskID)
	if err := child.Cancel(t0); err != nil {
		t.Fatal(err)
	}
	snapshot := child.Snapshot()
	snapshot.Input = json.RawMessage(`{"fromTeam":"qa"}`)
	broken, err := domain.RestoreTask(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := newAnswerDeliverer(h.deps).taskEnded(context.Background(), responder, broken, domain.Execution{}); err != nil {
		t.Fatal(err)
	}
	if audit := auditOf(h, "question.memory", "asked"); audit.Outcome != domain.AuditDenied || audit.Details["reason"] == "" {
		t.Errorf("audit = %+v", audit)
	}
	if len(answeredEvents(h, q.TaskID)) != 2 {
		t.Error("the answer did not go back")
	}
	messages, _ := NewExecutionMessages(h.deps).Execute(context.Background(), "qa-node", parent.ExecutionID, "")
	if len(messages) != 1 || messages[0].Memory != nil {
		t.Errorf("messages = %+v", messages)
	}
}

func TestChannelRecordFromPlainData(t *testing.T) {
	record := domain.ChannelRecord{
		Side: domain.ChannelAsked, Team: "qa", Other: "dev", Question: "task-1", Asker: "qa-jefe", Responder: "dev-a",
		Text: "Why?", Answer: "cancelled", Status: domain.TaskCancelled,
	}
	if got := channelRecordFrom(channelRecordData(record)); got == nil || *got != record {
		t.Fatalf("round trip = %+v", got)
	}
	for _, value := range []any{nil, "text", map[string]any{"side": "asked"}, func() {}} {
		if channelRecordFrom(value) != nil {
			t.Errorf("%v read as a record", value)
		}
	}
}

// auditOf is the last audit of the action on the side.
func auditOf(h *harness, action, side string) domain.AuditEntry {
	for i := len(h.log.audits) - 1; i >= 0; i-- {
		if a := h.log.audits[i]; a.Action == action && a.Details["side"] == side {
			return a
		}
	}
	return domain.AuditEntry{}
}

func mustTask(t *testing.T, h *harness, id domain.TaskID) domain.Task {
	t.Helper()
	task, err := h.tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
