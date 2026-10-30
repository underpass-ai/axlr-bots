package jev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// ReceiptSchema names the receipt a Jev execution leaves: every decision
// with a hash of the question, Jev's choice and probability, and what it
// cost.
const ReceiptSchema = "axlr-bots.jev-receipt/v1"

// Defaults of a run.
const (
	DefaultTimeout = 10 * time.Minute
	// toolTimeout bounds one tool call: a world's wait tool may hold a
	// call for a while before it answers.
	toolTimeout = 60 * time.Second
	// maxJevFailures ends a task whose questions Jev keeps not answering.
	maxJevFailures = 3
	// maxSameRefused ends a task whose world keeps asking the question
	// whose answer it refused: the program has no other move to make.
	maxSameRefused = 8
	// maxRefusals ends a task whose act the world keeps refusing in a row.
	maxRefusals = 3
	// DefaultIdleBackoff is the pause after a wait that answered with
	// neither an ask nor done, grown with each such wait up to
	// idleBackoffCap: a paused world holds the run without a tight loop
	// of waits eating the tool budget.
	DefaultIdleBackoff = time.Second
	idleBackoffCap     = 5
)

// Version is what the node reports as this runtime's.
const Version = "jev-runtime 1"

var firstNumber = regexp.MustCompile(`\d+`)

// Executor runs a Jev bot's tasks: application.RuntimeExecutor for the
// jev runtime. One execution is one fixed program over the bot's MCP
// server, each decision one question to Jev; the operator's and the
// teammates' events reach the run through Hear.
type Executor struct {
	// Judge asks Jev; nil refuses every execution with the reason.
	Judge Judge
	// Servers finds a program's MCP server.
	Servers MCPConfig
	// StateDir keeps the traces: <state>/traces/<execution>.jsonl.
	StateDir string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// IdleBackoff is the pause after an idle wait; zero is
	// DefaultIdleBackoff.
	IdleBackoff time.Duration

	mu      sync.Mutex
	running map[domain.ExecutionID]*run
}

var _ application.RuntimeExecutor = (*Executor)(nil)

// NewExecutor wires the runtime over the node's state.
func NewExecutor(judge Judge, servers MCPConfig, stateDir string) *Executor {
	return &Executor{Judge: judge, Servers: servers, StateDir: stateDir, running: map[domain.ExecutionID]*run{}}
}

// taskInput is what a task made by an event carries: the event's name
// and data (application.EmitEvent's fields).
type taskInput struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data"`
}

// Execute runs the program for the task and reports.
func (e *Executor) Execute(ctx context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	program, ok := request.Runtime.Jev()
	if !ok {
		return application.ExecutionReport{}, errors.New("jev: the request's runtime is not a jev program")
	}
	if e.Judge == nil {
		return application.ExecutionReport{}, errors.New("jev: this node has no TYPESAFE_API_KEY: it cannot run Jev bots")
	}
	address, err := e.Servers.Resolve(program.Server())
	if err != nil {
		return application.ExecutionReport{}, fmt.Errorf("jev: %w", err)
	}
	var input taskInput
	if len(request.Input) > 0 {
		_ = json.Unmarshal(request.Input, &input)
	}
	r := &run{executor: e, request: request, program: program, client: newMCPClient(address, toolTimeout), data: input.Data, event: input.Name}
	r.trace, err = e.openTrace(request.ExecutionID)
	if err != nil {
		return application.ExecutionReport{}, fmt.Errorf("jev: trace: %w", err)
	}
	defer r.trace.Close()
	e.mu.Lock()
	e.running[request.ExecutionID] = r
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.running, request.ExecutionID)
		e.mu.Unlock()
	}()
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	report := r.play(runCtx)
	if ctx.Err() != nil {
		// The node stopped the run (the lease was lost): its report would
		// be dropped, as AXLR's is.
		return report, ctx.Err()
	}
	return report, nil
}

// Hear takes the events that reached the execution while it runs: a cue
// changes what the next act passes on (the event's data, by the fields
// the program names). An event the execution's bot would answer with a
// task of its own (a stop.*) reaches it as a task, not here.
func (e *Executor) Hear(_ context.Context, execution domain.ExecutionID, messages []application.RelayedMessage) error {
	e.mu.Lock()
	r, ok := e.running[execution]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("jev: execution %s is not running here", execution)
	}
	for _, message := range messages {
		if message.Event == nil {
			continue
		}
		r.hear(message.Event.Name, message.Event.Data)
	}
	return nil
}

// TracePath returns the execution's trace file when one exists on disk.
func (e *Executor) TracePath(execution domain.ExecutionID) (string, bool) {
	path := e.tracePath(execution)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func (e *Executor) tracePath(execution domain.ExecutionID) string {
	return filepath.Join(e.StateDir, "traces", execution.String()+".jsonl")
}

func (e *Executor) openTrace(execution domain.ExecutionID) (*os.File, error) {
	path := e.tracePath(execution)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// run is one execution of the program.
type run struct {
	executor *Executor
	request  application.ExecutionRequest
	program  domain.JevProgram
	client   *mcpClient
	trace    *os.File

	mu    sync.Mutex
	event string
	data  map[string]any

	calls     []domain.ToolCall
	decisions []decision
	usage     domain.Usage
	failures  int
	phrases   []string
	// refused is the hash of the ask whose act the world last refused
	// (the same ask is not put to Jev again), and refusedTimes how many
	// acts in a row were refused.
	refused      string
	refusedTimes int
}

// decision is one question answered by Jev, as the receipt keeps it.
type decision struct {
	At          time.Time `json:"at"`
	AskSHA256   string    `json:"askSha256"`
	Choice      string    `json:"choice"`
	Probability float64   `json:"probability"`
	Confidence  float64   `json:"confidence"`
	InputTokens int       `json:"inputTokens"`
	Ms          int64     `json:"ms"`
	Acted       bool      `json:"acted"`
}

// hear keeps a later event's data for the next act: the cue's dynamics.
func (r *run) hear(name string, data map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log("heard", map[string]any{"event": name, "data": data})
	if r.data == nil {
		r.data = map[string]any{}
	}
	for key, value := range data {
		r.data[key] = value
	}
}

// pass is the event's data the act tool takes, by the program's fields.
func (r *run) pass() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]any{}
	for _, field := range r.program.Pass() {
		if value, ok := r.data[field]; ok && value != nil {
			out[field] = value
		}
	}
	return out
}

// play runs the program and builds the report.
func (r *run) play(ctx context.Context) application.ExecutionReport {
	started := r.executor.now()
	r.log("start", map[string]any{"event": r.event, "data": r.data, "program": r.program.Snapshot(), "server": r.client.url})
	outcome, reason := r.steps(ctx)
	ended := r.executor.now()
	r.log("end", map[string]any{"outcome": outcome, "reason": reason, "usage": r.usage})
	answer := reason
	if outcome == domain.ExecutionCompleted {
		answer = r.answer()
	}
	receipt, _ := json.Marshal(map[string]any{
		"schema": ReceiptSchema, "runtime": domain.RuntimeJev, "model": r.model(), "executionId": r.request.ExecutionID, "taskId": r.request.TaskID, "botId": r.request.BotID,
		"event": r.event, "startedAt": started, "endedAt": ended, "outcome": outcome, "decisions": r.decisions, "toolCalls": len(r.calls), "usage": r.usage,
	})
	return application.ExecutionReport{
		Outcome: outcome, FailureReason: reason, Receipt: receipt, TracePath: r.executor.tracePath(r.request.ExecutionID),
		Result: domain.Result{Answer: answer, SessionID: r.request.ExecutionID.String(), ToolCalls: r.calls}, Usage: r.usage,
		Policy:      &domain.PolicyRecord{Level: r.request.Level.String(), RuntimeLevel: "jev-program", Enforced: true},
		Correlation: &domain.CorrelationRecord{BotID: r.request.BotID.String(), TaskID: r.request.TaskID.String(), ExecutionID: r.request.ExecutionID.String(), TraceID: r.request.CorrelationID.String(), Carried: true},
	}
}

func (r *run) model() string {
	if client, ok := r.executor.Judge.(*Client); ok {
		return client.Model()
	}
	return r.program.Model()
}

// steps is the program: a stop.* task calls the stop tool once; any other
// starts, then asks Jev, acts and waits while the world asks.
func (r *run) steps(ctx context.Context) (domain.ExecutionOutcome, string) {
	if err := r.client.initialize(ctx, "axlr-node jev runtime", Version); err != nil {
		return domain.ExecutionFailed, err.Error()
	}
	if domain.KindOf(domain.EventName(r.event)) == domain.KindStop {
		if r.program.Stop() == "" {
			return domain.ExecutionCompleted, ""
		}
		if _, err := r.call(ctx, r.program.Stop(), nil); err != nil {
			return r.failed(ctx, err)
		}
		return domain.ExecutionCompleted, ""
	}
	result, err := r.call(ctx, r.program.Start(), nil)
	if err != nil {
		return r.failed(ctx, err)
	}
	idle := 0
	for {
		if ctx.Err() != nil {
			return domain.ExecutionFailed, "interrupted: " + ctx.Err().Error()
		}
		if r.usage.ToolCalls >= r.budget(r.request.Budgets.MaxToolCallsPerTask()) {
			return domain.ExecutionFailed, fmt.Sprintf("budget_exceeded: %d tool calls", r.usage.ToolCalls)
		}
		if done, _ := result.Object["done"].(bool); done {
			return domain.ExecutionCompleted, ""
		}
		ask, hasAsk := question(result.Object)
		switch {
		case hasAsk && r.refused != "" && r.refused == askHash(ask):
			// The same question whose answer the world just refused:
			// Jev would answer the same. Wait for the world to move on.
			idle++
		case hasAsk:
			idle = 0
			if r.usage.Requests >= r.budget(r.request.Budgets.MaxModelCallsPerTask()) {
				return domain.ExecutionFailed, fmt.Sprintf("budget_exceeded: %d Jev calls", r.usage.Requests)
			}
			verdict, err := r.judge(ctx, ask)
			if err != nil {
				if r.failures++; r.failures >= maxJevFailures {
					return domain.ExecutionFailed, "Jev did not answer " + strconv.Itoa(r.failures) + " times: " + err.Error()
				}
				break
			}
			r.failures = 0
			arguments := r.pass()
			arguments[r.program.Choice()] = choiceValue(verdict.Choice)
			if r.program.Verdict() != "" {
				arguments[r.program.Verdict()] = map[string]any{"choice": verdict.Choice, "probability": verdict.Probability, "confidence": verdict.Confidence}
			}
			acted, err := r.call(ctx, r.program.Act(), arguments)
			switch {
			case err != nil && !isToolError(err):
				return r.failed(ctx, err)
			case err != nil:
				r.refused, r.refusedTimes = askHash(ask), r.refusedTimes+1
				if r.refusedTimes >= maxRefusals {
					return domain.ExecutionFailed, fmt.Sprintf("%s refused %d times in a row: %s", r.program.Act(), r.refusedTimes, err.Error())
				}
			default:
				r.refused, r.refusedTimes = "", 0
				r.decisions[len(r.decisions)-1].Acted = true
				r.phrases = append(r.phrases, fmt.Sprintf("%s (%.2f)", verdict.Choice, verdict.Probability))
				if done, _ := acted.Object["done"].(bool); done {
					return domain.ExecutionCompleted, ""
				}
			}
		default:
			idle++
		}
		if r.refused != "" && idle > maxSameRefused {
			return domain.ExecutionFailed, fmt.Sprintf("%s was refused and the world asked the same question %d times", r.program.Act(), idle)
		}
		// Neither an ask nor done: the world is paused, or between
		// questions. Wait again, a little later each time; the budgets and
		// the timeout end a run the world never ends.
		if idle > 0 && !r.pause(ctx, time.Duration(min(idle, idleBackoffCap))*r.executor.idleBackoff()) {
			return domain.ExecutionFailed, "interrupted: " + ctx.Err().Error()
		}
		if result, err = r.call(ctx, r.program.Wait(), nil); err != nil {
			if isToolError(err) {
				result = ToolResult{Object: map[string]any{}}
				continue
			}
			return r.failed(ctx, err)
		}
	}
}

// pause sleeps unless ctx ends first; false means it ended.
func (r *run) pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (e *Executor) idleBackoff() time.Duration {
	if e.IdleBackoff > 0 {
		return e.IdleBackoff
	}
	return DefaultIdleBackoff
}

// failed is how a run ends on an error that is not a tool's refusal: an
// interruption is reported as such.
func (r *run) failed(ctx context.Context, err error) (domain.ExecutionOutcome, string) {
	if ctx.Err() != nil {
		return domain.ExecutionFailed, "interrupted: " + ctx.Err().Error()
	}
	return domain.ExecutionFailed, err.Error()
}

// budget is a budget as a bound: none is unbounded.
func (r *run) budget(n int) int {
	if n <= 0 {
		return int(^uint(0) >> 1)
	}
	return n
}

// call calls one tool, counts it and keeps it in the result's tool calls.
func (r *run) call(ctx context.Context, tool domain.ToolName, arguments map[string]any) (ToolResult, error) {
	r.usage.ToolCalls++
	id := fmt.Sprintf("tool-%d", r.usage.ToolCalls)
	started := time.Now()
	result, err := r.client.callTool(ctx, tool.String(), arguments)
	status := "completed"
	if err != nil {
		status = "failed"
	}
	r.calls = append(r.calls, domain.ToolCall{Name: tool.String(), CallID: id, Decision: "auto_approve", Status: status})
	entry := map[string]any{"tool": tool, "callId": id, "arguments": arguments, "ms": time.Since(started).Milliseconds(), "status": status}
	if err != nil {
		entry["error"] = err.Error()
	} else {
		entry["result"] = json.RawMessage(resultJSON(result))
	}
	r.log("tool", entry)
	return result, err
}

// askHash names a question: the same state, question and options.
func askHash(ask Question) string {
	raw, _ := json.Marshal(ask)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// judge puts the world's question to Jev, counts the call and keeps the
// decision for the receipt.
func (r *run) judge(ctx context.Context, ask Question) (Verdict, error) {
	r.usage.Requests++
	hash := askHash(ask)
	verdict, err := r.executor.Judge.Choose(ctx, ask)
	entry := map[string]any{"askSha256": hash, "question": ask.Question, "options": ask.Options, "ms": verdict.Took.Milliseconds()}
	if err != nil {
		entry["error"] = err.Error()
		r.log("jev", entry)
		return Verdict{}, err
	}
	r.usage.PromptTokens += verdict.InputTokens
	r.usage.CostUSD += float64(verdict.InputTokens) * InputPricePerToken
	r.decisions = append(r.decisions, decision{At: r.executor.now(), AskSHA256: hash, Choice: verdict.Choice, Probability: verdict.Probability, Confidence: verdict.Confidence, InputTokens: verdict.InputTokens, Ms: verdict.Took.Milliseconds()})
	entry["choice"], entry["probability"], entry["confidence"], entry["inputTokens"] = verdict.Choice, verdict.Probability, verdict.Confidence, verdict.InputTokens
	r.log("jev", entry)
	return verdict, nil
}

// answer is the one line a completed run reports: what Jev chose, with
// its probability for each.
func (r *run) answer() string {
	if len(r.phrases) == 0 {
		if r.event != "" {
			return "heard " + r.event + ": nothing to choose"
		}
		return "nothing to choose"
	}
	return "Jev chose: " + strings.Join(r.phrases, "; ")
}

// log appends one line to the trace.
func (r *run) log(kind string, fields map[string]any) {
	if r.trace == nil {
		return
	}
	entry := map[string]any{"type": kind, "at": r.executor.now().Format(time.RFC3339Nano)}
	for key, value := range fields {
		entry[key] = value
	}
	if line, err := json.Marshal(entry); err == nil {
		_, _ = r.trace.Write(append(line, '\n'))
	}
}

// question reads a tool result's ask {state, question, options}.
func question(object map[string]any) (Question, bool) {
	raw, ok := object["ask"].(map[string]any)
	if !ok {
		return Question{}, false
	}
	var q Question
	q.State, _ = raw["state"].(string)
	q.Question, _ = raw["question"].(string)
	if options, ok := raw["options"].([]any); ok {
		for _, option := range options {
			if text, ok := option.(string); ok {
				q.Options = append(q.Options, text)
			}
		}
	}
	return q, true
}

// choiceValue is the value the act tool takes for a choice: its first
// whole number ("phrase 3: bars 17–24" is 3), else its text.
func choiceValue(choice string) any {
	if match := firstNumber.FindString(choice); match != "" {
		if n, err := strconv.Atoi(match); err == nil {
			return n
		}
	}
	return choice
}

func isToolError(err error) bool {
	var refusal ToolError
	return errors.As(err, &refusal)
}

func resultJSON(result ToolResult) []byte {
	if result.Object != nil {
		if raw, err := json.Marshal(result.Object); err == nil {
			return raw
		}
	}
	raw, _ := json.Marshal(result.Text)
	return raw
}
