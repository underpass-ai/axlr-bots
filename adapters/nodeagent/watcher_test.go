package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/jev"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// offsetClock is the control plane's clock, moved forward by the test:
// real time, so heartbeats and leases go on, plus an offset.
type offsetClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *offsetClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

func (c *offsetClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

// fakeJudge is Jev choosing the same option every time, with a fixed
// verdict.
type fakeJudge struct{ choice string }

func (j fakeJudge) Choose(_ context.Context, q jev.Question) (jev.Verdict, error) {
	return jev.Verdict{Choice: j.choice, Probability: 0.83, Confidence: 0.71, InputTokens: 80, Took: time.Millisecond}, q.Validate()
}

// watcherDefinition is a team's Jev watcher on qa-node.
func watcherDefinition(id, team string) application.BotDefinition {
	return application.BotDefinition{
		ID: id, Tenant: "t", Name: id, Owner: "o", Mission: "watch the team's questions", Level: "act-within-policy",
		AllowedTools: []string{domain.WatchNextTool.String(), domain.WatchDecideTool.String()}, Groups: []string{team},
		Runtime: &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
			Server: domain.WatchServer, Start: domain.WatchNextTool.String(), Wait: domain.WatchNextTool.String(),
			Act: domain.JevActSnapshot{Tool: domain.WatchDecideTool.String(), Choice: domain.WatchChoice, Verdict: domain.WatchVerdict},
		}},
		Placement: domain.PlacementSnapshot{Nodes: []string{"qa-node"}}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 120},
	}
}

// stuckQuestion sets up the loop: qa-jefe on qa-node asks dev; dev-a's
// node is registered but runs no agent, so the question stays Pending;
// qa-node runs AXLR and Jev (with a judge that always chooses choice),
// and the watchers of qa and dev live there. The clock then moves past
// WatchAfter. It answers the question's task id.
func stuckQuestion(t *testing.T, choice string) (*fixture, string) {
	t.Helper()
	clock := &offsetClock{}
	f := controlWith(t, func(d *application.Dependencies) { d.Clock = clock })
	ctx := context.Background()
	askingTeams(t, f)
	for _, definition := range []application.BotDefinition{watcherDefinition("qa-vigia", "qa"), watcherDefinition("dev-vigia", "dev")} {
		if _, err := f.client.RegisterBot(ctx, definition); err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.EnableBot(ctx, definition.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.client.Register(ctx, application.NodeRegistration{ID: "dev-node", HostName: "dev-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, MemoryTeams: []string{"dev"}, TeamChannel: true}}); err != nil {
		t.Fatal(err)
	}
	scripted := &scriptedExecutor{tool: application.AskToolName, script: map[domain.BotID]map[string]any{"qa-jefe": {"team": "dev", "question": "Who owns the importer?"}}}
	runtimes := NewRuntimes(scripted)
	jevs := jev.NewExecutor(fakeJudge{choice: choice}, jev.MCPConfig{Path: filepath.Join(f.dir, "absent.json")}, filepath.Join(f.dir, "jev"))
	jevs.IdleBackoff = time.Millisecond
	runtimes.Add(domain.RuntimeJev, jevs)
	agent := Agent{
		Transport: f.client, Executor: runtimes,
		Options: Options{NodeID: "qa-node", HostName: "qa-node", Runtimes: runtimes.Kinds(), TeamChannel: true, Heartbeat: 50 * time.Millisecond, PollWait: 200 * time.Millisecond, Renew: 50 * time.Millisecond, SocketDir: shortDir(t) + "/run", Log: log.New(f.logs, "", 0)},
	}
	runCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(runCtx) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("qa-jefe", "Find who owns the importer"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	if len(parent.Questions) != 1 {
		t.Fatalf("questions = %v (%s)", parent.Questions, f.logs.String())
	}
	if question := waitFor(t, f.client, parent.Questions[0], "Pending"); question.BotID != "dev-a" {
		t.Fatalf("question = %+v", question)
	}
	clock.advance(application.WatchAfter + time.Minute)
	return f, parent.Questions[0]
}

// watch runs one round of the watcher and answers what its run said.
func watch(t *testing.T, f *fixture, bot string) string {
	t.Helper()
	submitted, err := f.client.SubmitTask(context.Background(), unixapi.SubmitRequest{BotID: bot, Objective: "Watch the team's questions", IdempotencyKey: "round-" + bot})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, f.client, submitted.Task.ID, "Completed")
	executions, err := f.client.ListExecutions(context.Background(), submitted.Task.ID)
	if err != nil || len(executions) != 1 {
		t.Fatalf("executions: %v %+v (%s)", err, executions, f.logs.String())
	}
	return executions[0].Result.Answer
}

// The whole loop: qa's Jev watcher is put qa-jefe's stuck question, Jev
// chooses abandon, the question is cancelled with Jev's verdict as the
// reason, and qa-jefe, which has ended, gets answered false naming the
// watcher as a task of its own.
func TestAJevWatcherAbandonsAStuckQuestionAndTheAskerHears(t *testing.T) {
	f, question := stuckQuestion(t, "abandon")
	// dev's watcher never sees qa's question: its round is done at once.
	if said := watch(t, f, "dev-vigia"); said != "nothing to choose" {
		t.Errorf("dev-vigia said %q", said)
	}
	waitFor(t, f.client, question, "Pending")
	if said := watch(t, f, "qa-vigia"); said != "Jev chose: abandon (0.83)" {
		t.Errorf("qa-vigia said %q (%s)", said, f.logs.String())
	}
	reason := "abandoned by qa-vigia: Jev chose abandon (p=0.83, c=0.71)"
	if task := waitFor(t, f.client, question, "Cancelled"); task.FailureReason != reason {
		t.Errorf("the question ended %q", task.FailureReason)
	}
	tasks, err := f.client.ListTasks(context.Background(), application.TaskQuery{BotID: "qa-jefe"})
	if err != nil {
		t.Fatal(err)
	}
	var back domain.TaskSnapshot
	for _, task := range tasks {
		if task.Trigger.Type == "question_answer" {
			back = task
		}
	}
	var facts map[string]any
	_ = json.Unmarshal(back.Input, &facts)
	if back.ID == "" || facts["answered"] != false || facts["reason"] != reason || !strings.Contains(back.Objective, "qa-vigia") {
		t.Fatalf("qa-jefe heard %+v (%s)", back, f.logs.String())
	}
	waitFor(t, f.client, back.ID, "Completed")
	if !strings.Contains(f.logs.String(), "watches its team's questions") || !strings.Contains(f.logs.String(), "question "+question+" "+reason) {
		t.Errorf("logs = %s", f.logs.String())
	}
}

// Jev chooses wait: the question stays as it was, and qa-jefe hears
// nothing.
func TestAJevWatcherThatWaitsChangesNothing(t *testing.T) {
	f, question := stuckQuestion(t, "wait")
	if said := watch(t, f, "qa-vigia"); said != "Jev chose: wait (0.83)" {
		t.Errorf("qa-vigia said %q (%s)", said, f.logs.String())
	}
	if task := waitFor(t, f.client, question, "Pending"); task.FailureReason != "" {
		t.Errorf("the question = %+v", task)
	}
	tasks, err := f.client.ListTasks(context.Background(), application.TaskQuery{BotID: "qa-jefe"})
	if err != nil || len(tasks) != 1 {
		t.Errorf("qa-jefe's tasks: %v %d", err, len(tasks))
	}
}

// watchTransport answers a watcher's calls from a script.
type watchTransport struct {
	fakeTransport
	next      []application.WatchedQuestion
	afters    []domain.TaskID
	decisions []application.WatchDecision
	err       error
}

func (w *watchTransport) WatchNext(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, after domain.TaskID) (application.WatchedQuestion, error) {
	w.afters = append(w.afters, after)
	if w.err != nil {
		return application.WatchedQuestion{}, w.err
	}
	if len(w.next) == 0 {
		return application.WatchedQuestion{Done: true, Team: "qa"}, nil
	}
	next := w.next[0]
	w.next = w.next[1:]
	return next, nil
}

func (w *watchTransport) DecideWatch(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, decision application.WatchDecision) (application.WatchDecided, error) {
	w.decisions = append(w.decisions, decision)
	if w.err != nil {
		return application.WatchDecided{}, w.err
	}
	return application.WatchDecided{Question: decision.Question, Choice: decision.Choice, Status: domain.TaskPending}, nil
}

// The node's watcher keeps the question being decided and the cursor: the
// next call goes on after the last question, a decision names the current
// one, and a decision without one is refused.
func TestTheNodesWatcherKeepsTheQuestionAndTheCursor(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	view := application.QuestionView{Question: "task-9", Team: "dev", AskedBy: "qa-jefe", Responder: "dev-a", Status: domain.TaskPending, AskedAt: at, Deadline: at.Add(2 * time.Hour), Now: at.Add(6 * time.Minute)}
	transport := &watchTransport{next: []application.WatchedQuestion{{Team: "qa", View: view, Cursor: "task-9"}}}
	w := &watcher{transport: transport, node: "qa-node", execution: "exec-1", log: log.New(&syncBuffer{}, "", 0)}
	ctx := context.Background()
	if refused, err := w.call(ctx, domain.WatchDecideTool.String(), nil); err != nil || !strings.Contains(refused.Refused, "call axlr_bots_watch_next first") {
		t.Errorf("decide first = %+v %v", refused, err)
	}
	put, err := w.call(ctx, domain.WatchNextTool.String(), nil)
	ask, _ := put.Object["ask"].(map[string]any)
	if err != nil || ask["question"] != "Should team qa keep waiting for this answer or abandon the question?" || !strings.Contains(ask["state"].(string), "Question task-9 from qa-jefe to team dev") {
		t.Fatalf("next = %+v %v", put, err)
	}
	decided, err := w.call(ctx, domain.WatchDecideTool.String(), map[string]any{"choice": "wait", "verdict": map[string]any{"choice": "wait", "probability": 0.6, "confidence": 0.5}})
	if err != nil || decided.Object["status"] != "Pending" || decided.Object["abandoned"] != false || decided.Object["done"] != nil {
		t.Errorf("decided = %+v %v", decided, err)
	}
	if d := transport.decisions[0]; d.Question != "task-9" || d.Choice != "wait" || d.Probability != 0.6 || d.Confidence != 0.5 {
		t.Errorf("decision = %+v", d)
	}
	if done, err := w.call(ctx, domain.WatchNextTool.String(), nil); err != nil || done.Object["done"] != true {
		t.Errorf("done = %+v %v", done, err)
	}
	if len(transport.afters) != 2 || transport.afters[0] != "" || transport.afters[1] != "task-9" {
		t.Errorf("afters = %v", transport.afters)
	}
	if other, err := w.call(ctx, "kmp_wake", nil); err != nil || other.Refused == "" {
		t.Errorf("another tool = %+v %v", other, err)
	}
	// A refusal of the control plane is the tool's error; a control
	// plane that cannot be reached is the run's.
	transport.err = domain.ForbiddenError{Reason: "bot qa-vigia is not a Jev watcher"}
	if refused, err := w.call(ctx, domain.WatchNextTool.String(), nil); err != nil || !strings.Contains(refused.Refused, "not a Jev watcher") {
		t.Errorf("refused = %+v %v", refused, err)
	}
	transport.err, transport.next = nil, []application.WatchedQuestion{{Team: "qa", View: view, Cursor: "task-9"}}
	if _, err := w.call(ctx, domain.WatchNextTool.String(), nil); err != nil {
		t.Fatal(err)
	}
	transport.err = errors.New("connection refused")
	if _, err := w.call(ctx, domain.WatchDecideTool.String(), map[string]any{"choice": "abandon"}); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("unreachable: %v", err)
	}
	transport.err = domain.NotFoundError{Kind: "question", ID: "task-9"}
	if refused, err := w.call(ctx, domain.WatchDecideTool.String(), map[string]any{"choice": "abandon"}); err != nil || refused.Refused == "" {
		t.Errorf("not found = %+v %v", refused, err)
	}
}

// An abandon is logged with its reason.
func TestTheNodesWatcherLogsAnAbandon(t *testing.T) {
	logs := &syncBuffer{}
	transport := &abandonTransport{}
	w := &watcher{transport: transport, node: "qa-node", execution: "exec-1", log: log.New(logs, "", 0), current: "task-9"}
	decided, err := w.call(context.Background(), domain.WatchDecideTool.String(), map[string]any{"choice": "abandon", "verdict": map[string]any{"probability": 0.83, "confidence": 0.71}})
	if err != nil || decided.Object["abandoned"] != true || decided.Object["reason"] != "abandoned by qa-vigia" || !strings.Contains(logs.String(), "question task-9 abandoned by qa-vigia") {
		t.Errorf("decided = %+v %v (%s)", decided, err, logs.String())
	}
}

type abandonTransport struct{ fakeTransport }

func (abandonTransport) DecideWatch(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, decision application.WatchDecision) (application.WatchDecided, error) {
	return application.WatchDecided{Question: decision.Question, Choice: decision.Choice, Status: domain.TaskCancelled, Abandoned: true, Reason: "abandoned by qa-vigia"}, nil
}
