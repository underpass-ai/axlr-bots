package jev

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// world is a fake queue service: an MCP server whose batches and listen
// ask which batch to process next until none is left, and whose process
// records what it was told.
type world struct {
	mu     sync.Mutex
	calls  []string
	acts   []map[string]any
	left   int
	refuse bool
	// moving asks a different question each time, even when nothing
	// was processed.
	moving   bool
	asked    int
	sessions int
	// afterFirstAct runs once, inside the first listen after a process,
	// without the lock: what the test does while the run waits.
	afterFirstAct func()
	// quiet is how many listens answer with neither an ask nor done
	// before the next question: a paused world.
	quiet int
}

func (w *world) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.ID == nil {
			rw.WriteHeader(http.StatusAccepted)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		answer := func(result any) {
			_ = json.NewEncoder(rw).Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(request.ID), "result": result})
		}
		if request.Method == "initialize" {
			w.mu.Lock()
			w.sessions++
			w.mu.Unlock()
			rw.Header().Set("Mcp-Session-Id", "s-1")
			answer(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "world"}})
			return
		}
		if request.Method != "tools/call" {
			_ = json.NewEncoder(rw).Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(request.ID), "error": map[string]any{"code": -32601, "message": "method not found"}})
			return
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.calls = append(w.calls, request.Params.Name)
		if request.Params.Name == "listen" && len(w.acts) == 1 && w.afterFirstAct != nil {
			hook := w.afterFirstAct
			w.afterFirstAct = nil
			w.mu.Unlock()
			hook()
			w.mu.Lock()
		}
		text := func(out map[string]any, isError bool) {
			raw, _ := json.Marshal(out)
			answer(map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "isError": isError})
		}
		switch request.Params.Name {
		case "batches", "listen":
			if request.Params.Name == "listen" && w.quiet > 0 {
				w.quiet--
				text(map[string]any{"state": "paused", "note": "the operator holds the queue"}, false)
				return
			}
			if w.left == 0 {
				text(map[string]any{"note": "no batch left to process", "done": true}, false)
				return
			}
			n := 4 - w.left
			w.asked++
			state := "A queue runs. This worker has nothing scheduled."
			if w.moving {
				state += " Row " + itoa(w.asked%10)
			}
			text(map[string]any{"ask": map[string]any{
				"state": state, "question": "Which batch next?",
				"options": []string{"batch " + itoa(n) + ": rows 1–8", "batch " + itoa(n+1) + ": rows 9–16"},
			}}, false)
		case "process":
			if w.refuse {
				text(map[string]any{"error": "the operator stopped the queue"}, true)
				return
			}
			w.left--
			w.acts = append(w.acts, request.Params.Arguments)
			text(map[string]any{"note": "batch runs in 1.2 s"}, false)
		case "stop":
			text(map[string]any{"state": "stopped", "done": true}, false)
		default:
			text(map[string]any{"error": "unknown tool"}, true)
		}
	}))
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n))) }

// jevServer is a fake TypeSafe that picks the first option.
func jevServer(t *testing.T, pick func(options []string) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		var request struct {
			Model     string `json:"model"`
			Questions map[string]struct {
				Criteria map[string]any `json:"criteria"`
			} `json:"questions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		var options []string
		for option := range request.Questions["q"].Criteria {
			options = append(options, option)
		}
		choice := pick(options)
		probabilities := map[string]float64{}
		for _, option := range options {
			probabilities[option] = 0.1
		}
		probabilities[choice] = 0.9
		_ = json.NewEncoder(rw).Encode(map[string]any{"model": request.Model, "usage": map[string]any{"input_tokens": 120},
			"answers": map[string]any{"q": map[string]any{"type": "choice", "choice": choice, "probabilities": probabilities, "confidence": 0.8}}})
	}))
}

func program(t *testing.T) domain.Runtime {
	t.Helper()
	runtime, err := domain.NewRuntime(&domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
		Server: "queue", Start: "batches", Wait: "listen", Stop: "stop",
		Act:  domain.JevActSnapshot{Tool: "process", Choice: "batch", Verdict: "judge", Pass: []string{"priority"}},
		Tell: &domain.JevTellSnapshot{Name: "info.worker.processed", Fields: []string{"batch", "probability"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func fixture(t *testing.T, w *world, pick func([]string) string) (*Executor, application.ExecutionRequest) {
	t.Helper()
	server := w.serve(t)
	t.Cleanup(server.Close)
	typesafe := jevServer(t, pick)
	t.Cleanup(typesafe.Close)
	dir := t.TempDir()
	config := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(filepath.Join(dir, "queue.json"), []byte(`{"id": "queue", "url": "`+server.URL+`/mcp/worker-1"}`), 0o600)
	_ = os.WriteFile(config, []byte(`{"version": 1, "plugins": [{"manifest": "queue.json", "name": "queue"}]}`), 0o600)
	judge, err := NewClient(ClientConfig{APIKey: "test-key", Endpoint: typesafe.URL})
	if err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(judge, MCPConfig{Path: config}, filepath.Join(dir, "node"))
	executor.IdleBackoff = time.Millisecond
	budgets, _ := domain.NewBudgets(16, 32, 0, time.Minute)
	input, _ := json.Marshal(map[string]any{"name": "info.notice.load", "data": map[string]any{"priority": "high"}, "message": "load"})
	request := application.ExecutionRequest{
		ExecutionID: "exec-1", TaskID: "task-1", BotID: "worker-1", TenantID: "default", CorrelationID: "trace-1",
		Level: domain.AutonomyActWithinPolicy, Runtime: program(t), Input: input, Budgets: budgets, Timeout: 30 * time.Second,
	}
	return executor, request
}

func first(options []string) string {
	best := options[0]
	for _, option := range options {
		if option < best {
			best = option
		}
	}
	return best
}

// A notice runs the program: batches, then Jev → process → listen until
// the world says done. Each process carries the batch number from Jev's
// choice, Jev's verdict and the notice's priority; the receipt lists each
// decision.
func TestANoticeIsProcessedBatchByBatchAsJevChooses(t *testing.T) {
	w := &world{left: 3}
	executor, request := fixture(t, w, first)
	report, err := executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if got := strings.Join(w.calls, " "); got != "batches process listen process listen process listen" {
		t.Errorf("calls = %s", got)
	}
	if len(w.acts) != 3 || w.acts[0]["batch"] != float64(1) || w.acts[0]["priority"] != "high" {
		t.Errorf("acts = %v", w.acts)
	}
	verdict, _ := w.acts[0]["judge"].(map[string]any)
	if verdict["choice"] != "batch 1: rows 1–8" || verdict["probability"] != 0.9 || verdict["confidence"] != 0.8 {
		t.Errorf("verdict = %v", verdict)
	}
	if !strings.HasPrefix(report.Result.Answer, "Jev chose: batch 1: rows 1–8 (0.90); batch 2") {
		t.Errorf("answer = %q", report.Result.Answer)
	}
	if report.Usage.Requests != 3 || report.Usage.ToolCalls != 7 || report.Usage.PromptTokens != 360 || report.Usage.CostUSD <= 0 {
		t.Errorf("usage = %+v", report.Usage)
	}
	var receipt struct {
		Schema    string           `json:"schema"`
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal(report.Receipt, &receipt); err != nil || receipt.Schema != ReceiptSchema || len(receipt.Decisions) != 3 || receipt.Decisions[0]["acted"] != true {
		t.Errorf("receipt = %s %v", report.Receipt, err)
	}
	if report.Policy == nil || !report.Policy.Enforced || report.Correlation == nil || report.Correlation.TraceID != "trace-1" {
		t.Errorf("policy %+v correlation %+v", report.Policy, report.Correlation)
	}
	if path, ok := executor.TracePath("exec-1"); !ok || report.TracePath != path {
		t.Errorf("trace = %q %v", report.TracePath, ok)
	} else if raw, _ := os.ReadFile(path); strings.Count(string(raw), `"type":"jev"`) != 3 || !strings.Contains(string(raw), `"type":"end"`) {
		t.Errorf("trace lines:\n%s", raw)
	}
	if w.sessions != 1 {
		t.Errorf("sessions = %d", w.sessions)
	}
}

// A world that answers listen with neither an ask nor done (a pause) is
// waited on again, a little later each time, and the run goes on when the
// next question comes: a pause never ends a run.
func TestAPausedWorldHoldsTheRun(t *testing.T) {
	w := &world{left: 2, quiet: 12}
	executor, request := fixture(t, w, first)
	report, err := executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionCompleted || len(w.acts) != 2 {
		t.Fatalf("report = %+v %v acts %d", report, err, len(w.acts))
	}
	if got := strings.Count(strings.Join(w.calls, " "), "listen"); got != 14 {
		t.Errorf("listens = %d", got)
	}
	// A world that never asks again is ended by the budgets or the task's
	// timeout, whichever comes first, never completed by itself, and Jev
	// is not asked meanwhile.
	w = &world{left: 2, quiet: 1 << 20}
	executor, request = fixture(t, w, first)
	request.Timeout = 300 * time.Millisecond
	report, err = executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionFailed || report.Usage.Requests != 1 || !(strings.HasPrefix(report.FailureReason, "interrupted") || strings.HasPrefix(report.FailureReason, "budget_exceeded")) {
		t.Fatalf("report = %+v %v", report, err)
	}
}

// After each act the program tells its teams, through the node's emit,
// the fields it names; a tell that is refused or fails is logged and
// the run goes on. Without a team (no OnEmit) nothing is told.
func TestEachActIsToldToTheTeam(t *testing.T) {
	w := &world{left: 2}
	executor, request := fixture(t, w, first)
	var told []application.EmitEventCommand
	request.OnEmit = func(command application.EmitEventCommand) (application.EmitAnswer, error) {
		told = append(told, command)
		if len(told) == 2 {
			return application.EmitAnswer{Refused: &domain.EventRefusal{Reason: "rate"}}, nil
		}
		return application.EmitAnswer{EventID: "evt-1", Deliveries: []application.EventDelivery{{BotID: "worker-2"}}}, nil
	}
	report, err := executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionCompleted || len(told) != 2 {
		t.Fatalf("report = %+v %v told %d", report, err, len(told))
	}
	var data map[string]any
	_ = json.Unmarshal(told[0].Data, &data)
	if told[0].Name != "info.worker.processed" || told[0].Message != "batch 2: rows 1–8" || data["batch"] != float64(2) || data["probability"] != 0.9 || len(data) != 2 {
		t.Errorf("told = %+v %v", told[0], data)
	}
	tells := 0
	for _, call := range report.Result.ToolCalls {
		if call.Name == application.EmitEventToolName {
			tells++
		}
	}
	if tells != 2 || report.Usage.ToolCalls != 7 {
		t.Errorf("tool calls = %+v", report.Result.ToolCalls)
	}
	w = &world{left: 1}
	executor, request = fixture(t, w, first)
	if report, _ := executor.Execute(context.Background(), request); report.Usage.ToolCalls != 3 {
		t.Errorf("without a team: %+v", report.Result.ToolCalls)
	}
}

// A stop.* task calls the stop tool once and ends.
func TestAStopTaskCallsStopOnce(t *testing.T) {
	w := &world{left: 3}
	executor, request := fixture(t, w, first)
	request.Input, _ = json.Marshal(map[string]any{"name": "stop.halt", "message": "stop"})
	report, err := executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionCompleted || strings.Join(w.calls, " ") != "stop" {
		t.Fatalf("report = %+v %v calls %v", report, err, w.calls)
	}
	if report.Result.Answer != "heard stop.halt: nothing to choose" {
		t.Errorf("answer = %q", report.Result.Answer)
	}
}

// A notice heard while the run works changes what the next process
// passes on.
func TestANoticeHeardWhileItRunsChangesThePriority(t *testing.T) {
	w := &world{left: 2}
	executor, request := fixture(t, w, first)
	heard := false
	// The notice arrives while the run waits in listen, as a real one does.
	w.afterFirstAct = func() {
		err := executor.Hear(context.Background(), "exec-1", []application.RelayedMessage{{ID: "evt-1", Text: "...", Event: &application.RelayedEvent{Name: "info.notice.load", Data: map[string]any{"priority": "low"}}}})
		if err != nil {
			t.Error(err)
		}
		heard = true
	}
	report, err := executor.Execute(context.Background(), request)
	if !heard {
		t.Fatal("the notice was never heard")
	}
	if err != nil || report.Outcome != domain.ExecutionCompleted || len(w.acts) != 2 {
		t.Fatalf("report = %+v %v acts %v", report, err, w.acts)
	}
	if w.acts[0]["priority"] != "high" || w.acts[1]["priority"] != "low" {
		t.Errorf("priority = %v %v", w.acts[0]["priority"], w.acts[1]["priority"])
	}
	if err := executor.Hear(context.Background(), "exec-9", nil); err == nil {
		t.Error("an unknown execution heard a message")
	}
}

// The budgets end a run: too many Jev calls or tool calls is Failed with
// the reason, as AXLR's budget_exceeded is.
func TestTheBudgetsBoundARun(t *testing.T) {
	w := &world{left: 10}
	executor, request := fixture(t, w, first)
	request.Budgets, _ = domain.NewBudgets(2, 0, 0, time.Minute)
	report, err := executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionFailed || !strings.HasPrefix(report.FailureReason, "budget_exceeded: 2 Jev calls") {
		t.Fatalf("report = %+v %v", report, err)
	}
	w = &world{left: 10}
	executor, request = fixture(t, w, first)
	request.Budgets, _ = domain.NewBudgets(0, 3, 0, time.Minute)
	if report, _ := executor.Execute(context.Background(), request); report.Outcome != domain.ExecutionFailed || !strings.HasPrefix(report.FailureReason, "budget_exceeded: 3 tool calls") {
		t.Errorf("report = %+v", report)
	}
}

// A refusal of the act tool is a step that did not happen: the run
// listens on. A node that stops the run (its context) ends it as
// interrupted, with an error so the agent's report is dropped.
func TestARefusedActIsNotFatalAndACancelInterrupts(t *testing.T) {
	w := &world{left: 2, refuse: true}
	executor, request := fixture(t, w, first)
	report, err := executor.Execute(context.Background(), request)
	// Every listen asks the same question again and every process is refused:
	// Jev is asked once for it, the run waits for the world to move on,
	// and ends when it does not.
	if err != nil || report.Outcome != domain.ExecutionFailed || !strings.Contains(report.FailureReason, "process was refused") || report.Usage.Requests != 1 {
		t.Fatalf("report = %+v %v", report, err)
	}
	w = &world{left: 2, refuse: true, moving: true}
	executor, request = fixture(t, w, first)
	report, _ = executor.Execute(context.Background(), request)
	// A world that asks a new question each time has each answer refused:
	// three refusals in a row end the run.
	if report.Outcome != domain.ExecutionFailed || !strings.Contains(report.FailureReason, "refused 3 times in a row") || report.Usage.Requests != 3 {
		t.Fatalf("report = %+v", report)
	}
	w = &world{left: 100}
	executor, request = fixture(t, w, first)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.afterFirstAct = cancel
	report, err = executor.Execute(ctx, request)
	if !errors.Is(err, context.Canceled) || report.Outcome != domain.ExecutionFailed || !strings.HasPrefix(report.FailureReason, "interrupted") {
		t.Errorf("report = %+v %v", report, err)
	}
}

// Without Jev's key, or without the program's server, nothing runs and
// the reason says why.
func TestARunNeedsJevAndTheServer(t *testing.T) {
	w := &world{left: 1}
	executor, request := fixture(t, w, first)
	executor.Judge = nil
	if _, err := executor.Execute(context.Background(), request); err == nil || !strings.Contains(err.Error(), "TYPESAFE_API_KEY") {
		t.Errorf("without a judge: %v", err)
	}
	executor, request = fixture(t, w, first)
	executor.Servers = MCPConfig{Path: filepath.Join(t.TempDir(), "none.json")}
	if _, err := executor.Execute(context.Background(), request); err == nil || !strings.Contains(err.Error(), "MCP configuration") {
		t.Errorf("without the server: %v", err)
	}
	request.Runtime = domain.Runtime{}
	if _, err := executor.Execute(context.Background(), request); err == nil || !strings.Contains(err.Error(), "not a jev program") {
		t.Errorf("an axlr request: %v", err)
	}
}

// Jev not answering is retried on the next question; three in a row end
// the run.
func TestJevNotAnsweringEndsTheRunAfterThreeTimes(t *testing.T) {
	w := &world{left: 10}
	executor, request := fixture(t, w, first)
	down := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(http.StatusBadGateway) }))
	defer down.Close()
	executor.Judge, _ = NewClient(ClientConfig{APIKey: "test-key", Endpoint: down.URL})
	report, err := executor.Execute(context.Background(), request)
	if err != nil || report.Outcome != domain.ExecutionFailed || !strings.Contains(report.FailureReason, "Jev did not answer 3 times") {
		t.Fatalf("report = %+v %v", report, err)
	}
	if got := strings.Join(w.calls, " "); got != "batches listen listen" {
		t.Errorf("calls = %s", got)
	}
}

func TestChoiceValueAndQuestions(t *testing.T) {
	if choiceValue("batch 3: rows 17–24") != 3 || choiceValue("yes") != "yes" {
		t.Error("choice values")
	}
	if _, ok := question(map[string]any{"note": "x"}); ok {
		t.Error("a result without ask has a question")
	}
	q, ok := question(map[string]any{"ask": map[string]any{"state": "s", "question": "q", "options": []any{"a", 1, "b"}}})
	if !ok || len(q.Options) != 2 || q.Options[1] != "b" {
		t.Errorf("question = %+v %v", q, ok)
	}
}
