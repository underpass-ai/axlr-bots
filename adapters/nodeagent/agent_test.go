package nodeagent

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/adapters/axlr"
	"github.com/underpass-ai/underpass-bots/adapters/unixapi"
	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/compose"
	"github.com/underpass-ai/underpass-bots/domain"
	"github.com/underpass-ai/underpass-bots/internal/fakeaxlr"
)

var _ Executor = (*axlr.Executor)(nil)

type fixture struct {
	dir     string
	socket  string
	client  *unixapi.Client
	cancel  context.CancelFunc
	served  chan error
	logs    *bytes.Buffer
	marker  string
	nodeDir string
}

// control starts a control plane on a socket with the fake axlr for the
// node, and returns the fixture.
func control(t *testing.T, scenario string) *fixture {
	t.Helper()
	dir := t.TempDir()
	deps, err := compose.Dependencies(filepath.Join(dir, "cp"), "/nonexistent/axlr-tui", "test:cp")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "cp.sock")
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- unixapi.NewServer(unixapi.NewLocal(deps)).Serve(ctx, socket) }()
	deadline := time.Now().Add(5 * time.Second)
	for !unixapi.Reachable(context.Background(), socket) {
		if time.Now().After(deadline) {
			t.Fatal("control plane not reachable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() { cancel(); <-served })
	return &fixture{dir: dir, socket: socket, client: unixapi.NewClient(socket), cancel: cancel, served: served, logs: &bytes.Buffer{}, marker: filepath.Join(dir, "marker"), nodeDir: filepath.Join(dir, "node")}
}

// agent builds the node agent with the fake axlr scenario.
func (f *fixture) agent(t *testing.T, scenario string) Agent {
	t.Helper()
	bin, _, err := fakeaxlr.Write(f.dir, fakeaxlr.Options{Scenario: scenario, Marker: f.marker})
	if err != nil {
		t.Fatal(err)
	}
	return Agent{
		Transport: f.client,
		Executor:  axlr.NewExecutor(bin, f.nodeDir),
		Options:   Options{NodeID: "web-1", HostName: "web-1.example", Labels: map[string]string{"host": "web-1"}, Heartbeat: 50 * time.Millisecond, PollWait: 200 * time.Millisecond, Renew: 30 * time.Millisecond, Log: log.New(f.logs, "", 0)},
	}
}

func waitFor(t *testing.T, client *unixapi.Client, id, status string) domain.TaskSnapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		task, err := client.GetTask(context.Background(), id)
		if err == nil && task.Status == status {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s is %s (%v), want %s", id, task.Status, err, status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAgentRunsAssignedTasksAndDecisions(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	agent := f.agent(t, "approval-flow")
	agentCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if node, err := f.client.GetNode(ctx, "web-1"); err == nil && node.Status == "Online" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node not registered: %s", f.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	node, _ := f.client.GetNode(ctx, "web-1")
	if !node.Capabilities.PolicySupported || node.HostName != "web-1.example" || node.Labels["host"] != "web-1" {
		t.Errorf("node = %+v", node)
	}
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "do what each task asks", Level: "act-with-approval", AllowedTools: []string{"local_read", "local_write"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := f.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "write hello.txt", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := waitFor(t, f.client, submitted.Task.ID, "WaitingApproval")
	execution, _ := f.client.GetExecution(ctx, waiting.ExecutionIDs[0])
	if execution.NodeID != "web-1" || execution.Outcome != "DecisionRequired" || execution.Result.PendingDecision == nil || !strings.HasPrefix(execution.TracePath, f.nodeDir) {
		t.Errorf("execution = %+v", execution)
	}
	if _, err := os.Stat(f.marker); err == nil {
		t.Fatal("the write ran before the approval")
	}
	decided, err := f.client.DecideTask(ctx, submitted.Task.ID, "approve")
	if err != nil || decided.Task.Status != "Running" || decided.Execution.NodeID != "web-1" || decided.Execution.Kind != "decide" {
		t.Fatalf("decide: %v %+v", err, decided)
	}
	completed := waitFor(t, f.client, submitted.Task.ID, "Completed")
	if len(completed.ExecutionIDs) != 2 {
		t.Errorf("task = %+v", completed)
	}
	if _, err := os.Stat(f.marker); err != nil {
		t.Error("the write did not run after the approval")
	}
	if report, err := f.client.GetReport(ctx, submitted.Task.ID); err != nil || !strings.Contains(report.Content, "Restarted") {
		t.Errorf("report = %+v %v", report, err)
	}
	logs := f.logs.String()
	for _, want := range []string{"node: registered as web-1", "assigned, lease until", "reported DecisionRequired (accepted true)", "reported Completed (accepted true)"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q:\n%s", want, logs)
		}
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func TestAgentRetriesRegistrationAndReportsRuntimeErrors(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	// A control plane that forgets the node (fresh state) gets a new
	// registration on the next heartbeat; an unreachable one is retried.
	stale := Agent{Transport: unixapi.NewClient(filepath.Join(f.dir, "nowhere.sock")), Executor: f.agent(t, "completed").Executor, Options: Options{NodeID: "web-1", Log: log.New(f.logs, "", 0)}}
	staleCtx, cancelStale := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelStale()
	if err := stale.Run(staleCtx); err != nil {
		t.Errorf("unreachable control plane: %v", err)
	}
	if !strings.Contains(f.logs.String(), "register:") {
		t.Errorf("logs = %s", f.logs.String())
	}
	agent := f.agent(t, "failed")
	agentCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	defer func() { stop(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if node, err := f.client.GetNode(ctx, "web-1"); err == nil && node.Status == "Online" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("node not registered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := f.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "o", IdempotencyKey: "k"})
	failed := waitFor(t, f.client, submitted.Task.ID, "Failed")
	if !strings.Contains(failed.FailureReason, "no model provider") {
		t.Errorf("task = %+v", failed)
	}
	// Forget the node on the control plane: the heartbeat re-registers it.
	if err := os.Remove(filepath.Join(f.dir, "cp", "nodes", "web-1.json")); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if node, err := f.client.GetNode(ctx, "web-1"); err == nil && node.Status == "Online" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node not re-registered: %s", f.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAgentRefusals(t *testing.T) {
	if err := (Agent{}).Run(context.Background()); err == nil {
		t.Error("no transport accepted")
	}
	f := control(t, "completed")
	agent := f.agent(t, "completed")
	agent.Options.NodeID = "Bad Id"
	if err := agent.Run(context.Background()); err == nil {
		t.Error("bad node id accepted")
	}
	o := Options{}
	if o.heartbeat() != DefaultHeartbeat || o.pollWait() != DefaultPollWait || o.renew() != DefaultRenew || o.log() == nil {
		t.Error("defaults wrong")
	}
	// An assignment with neither a run nor a decision is reported failed.
	fake := &fakeTransport{}
	broken := Agent{Transport: fake, Executor: agent.Executor, Options: Options{NodeID: "web-1", Log: log.New(f.logs, "", 0)}}
	broken.carry(context.Background(), "web-1", application.Assignment{ExecutionID: "exec-1"})
	if fake.report.Outcome != domain.ExecutionFailed || !strings.Contains(fake.report.FailureReason, "neither") {
		t.Errorf("report = %+v", fake.report)
	}
	// A report the control plane does not know is not retried.
	fake.notFound = true
	broken.report("web-1", "exec-2", application.ExecutionReport{Outcome: domain.ExecutionCompleted})
	if fake.reports != 2 {
		t.Errorf("reports = %d", fake.reports)
	}
}

type fakeTransport struct {
	report   application.ExecutionReport
	reports  int
	notFound bool
}

func (f *fakeTransport) Register(context.Context, application.NodeRegistration) (domain.NodeSnapshot, error) {
	return domain.NodeSnapshot{}, nil
}
func (f *fakeTransport) Heartbeat(context.Context, domain.NodeID) (domain.NodeSnapshot, error) {
	return domain.NodeSnapshot{}, nil
}
func (f *fakeTransport) NextAssignment(context.Context, domain.NodeID, time.Duration) (application.Assignment, bool, error) {
	return application.Assignment{}, false, nil
}
func (f *fakeTransport) RenewLease(context.Context, domain.NodeID, domain.ExecutionID) (time.Time, error) {
	return time.Time{}, nil
}
func (f *fakeTransport) Report(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, report application.ExecutionReport) (application.ReportAck, error) {
	f.reports++
	f.report = report
	if f.notFound {
		return application.ReportAck{}, domain.NotFoundError{Kind: "execution", ID: "exec-2"}
	}
	return application.ReportAck{Accepted: true, Outcome: report.Outcome}, nil
}
