package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

var _ Executor = (*axlr.Executor)(nil)

type fixture struct {
	dir     string
	deps    application.Dependencies
	socket  string
	client  *unixapi.Client
	cancel  context.CancelFunc
	served  chan error
	logs    *syncBuffer
	marker  string
	nodeDir string
}

// syncBuffer is a log sink the agent's goroutines write and the test
// reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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
	return &fixture{dir: dir, deps: deps, socket: socket, client: unixapi.NewClient(socket), cancel: cancel, served: served, logs: &syncBuffer{}, marker: filepath.Join(dir, "marker"), nodeDir: filepath.Join(dir, "node")}
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
	decided, err := f.client.DecideTask(ctx, submitted.Task.ID, unixapi.DecideRequest{Decision: "approve"})
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
	// The task is Completed once the control plane took the report; the
	// agent writes its log line after the answer comes back, so wait for
	// it rather than read the buffer once.
	for _, want := range []string{"node: registered as web-1", "assigned, lease until", "reported DecisionRequired (accepted true)", "reported Completed (accepted true)"} {
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(f.logs.String(), want) {
			if time.Now().After(deadline) {
				t.Errorf("logs lack %q:\n%s", want, f.logs.String())
				break
			}
			time.Sleep(10 * time.Millisecond)
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
	uploads  []string
}

func (f *fakeTransport) UploadArtifact(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, artifact domain.Artifact, content io.Reader) error {
	data, err := io.ReadAll(content)
	f.uploads = append(f.uploads, artifact.Name+"="+string(data))
	return err
}

func (f *fakeTransport) Delegate(context.Context, domain.NodeID, domain.ExecutionID, application.DelegateCommand) (application.DelegationAnswer, error) {
	return application.DelegationAnswer{}, errors.New("not served")
}
func (f *fakeTransport) AwaitDelegation(context.Context, domain.NodeID, domain.ExecutionID, domain.TaskID, time.Duration) (application.DelegationAnswer, error) {
	return application.DelegationAnswer{}, errors.New("not served")
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

// TestAgentHeartbeatsWhileItRuns gives the node a run longer than the
// control plane's offline threshold and checks the node stays Online:
// the heartbeat is not blocked by the execution.
func TestAgentHeartbeatsWhileItRuns(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	bin, _, err := fakeaxlr.Write(f.dir, fakeaxlr.Options{Scenario: "slow"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{Transport: f.client, Executor: axlr.NewExecutor(bin, f.nodeDir), Options: Options{NodeID: "web-1", Heartbeat: 30 * time.Millisecond, PollWait: 100 * time.Millisecond, Renew: 30 * time.Millisecond, Log: log.New(f.logs, "", 0)}}
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
	waitFor(t, f.client, submitted.Task.ID, "Running")
	before, _ := f.client.GetNode(ctx, "web-1")
	time.Sleep(200 * time.Millisecond)
	after, _ := f.client.GetNode(ctx, "web-1")
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Errorf("no heartbeat while running: %s then %s", before.LastSeenAt, after.LastSeenAt)
	}
	waitFor(t, f.client, submitted.Task.ID, "Completed")
}

func TestAgentStopsARunWhoseLeaseIsLost(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	agent := f.agent(t, "slow")
	agentCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "do what each task asks", AllowedTools: []string{"local_read"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := f.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "take a while", IdempotencyKey: "k-slow"})
	if err != nil {
		t.Fatal(err)
	}
	running := waitFor(t, f.client, submitted.Task.ID, "Running")
	// The task is Running as soon as the control plane creates the
	// assignment, before the node takes it. Cancelled in that moment, the
	// node is given nothing to run and no lease is ever lost (1 run in 40
	// on one CPU, 25 Oct 2026): wait until the node has the run.
	assigned := "execution " + running.ExecutionIDs[0] + " (run) assigned"
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(f.logs.String(), assigned); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the node never took the run:\n%s", f.logs.String())
		}
	}
	node, _ := f.client.GetNode(ctx, "web-1")
	if node.Capabilities.CorrelationSupported {
		t.Error("the stand-in does not take the correlation")
	}
	// The operator cancels the task while the node runs it: the next lease
	// renewal is refused, the agent stops the run and its report is dropped.
	cancelled, err := application.NewCancelTask(f.deps).Execute(ctx, domain.TaskID(submitted.Task.ID))
	if err != nil || cancelled.Execution == nil || cancelled.Execution.NodeID() != "web-1" {
		t.Fatalf("cancel: %v %+v", err, cancelled)
	}
	for _, want := range []string{"the lease is lost, stopping the run", "(accepted false)"} {
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(f.logs.String(), want) {
			if time.Now().After(deadline) {
				t.Errorf("logs lack %q:\n%s", want, f.logs.String())
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	task, _ := f.client.GetTask(ctx, submitted.Task.ID)
	execution, _ := f.client.GetExecution(ctx, running.ExecutionIDs[0])
	if task.Status != "Cancelled" || execution.Outcome != "Cancelled" || execution.Correlation == nil || execution.Correlation.TraceID != task.CorrelationID {
		t.Errorf("task %s, execution %+v", task.Status, execution)
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

func TestAgentUploadsWhatTheExecutionDeliveredBeforeItReports(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	agent := f.agent(t, "delivered")
	agentCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	defer func() { stop(); <-done }()
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "m", AllowedTools: []string{"local_read", "axlr_deliver"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := f.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "write the notes", IdempotencyKey: "k"})
	completed := waitFor(t, f.client, submitted.Task.ID, "Completed")
	executions, err := f.client.ListExecutions(ctx, completed.ID)
	if err != nil || len(executions) != 1 {
		t.Fatalf("executions: %v %d", err, len(executions))
	}
	execution := executions[0]
	artifacts := execution.Result.Artifacts
	if len(artifacts) != 3 || artifacts[0].Name != "notes.md" || !artifacts[0].Stored || artifacts[1].Name != "plan.md" || artifacts[1].Stored || artifacts[2].Name != "report.md" {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	if !strings.Contains(string(execution.Receipt), `"axlr.execution-receipt/v1"`) {
		t.Errorf("receipt = %s", execution.Receipt)
	}
	content, err := f.client.GetArtifact(ctx, execution.ID, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(content.Body)
	_ = content.Body.Close()
	if string(data) != fakeaxlr.DeliveredContent || content.Artifact.MediaType != "text/markdown" || content.Artifact.SHA256 != artifacts[0].SHA256 {
		t.Errorf("served %q %+v", data, content.Artifact)
	}
	if _, err := f.client.GetArtifact(ctx, execution.ID, "plan.md"); !domain.IsNotFound(err) {
		t.Errorf("an artifact that did not match was served: %v", err)
	}
	for _, want := range []string{"artifact notes.md uploaded", "artifact plan.md not uploaded"} {
		if !strings.Contains(f.logs.String(), want) {
			t.Errorf("logs lack %q:\n%s", want, f.logs.String())
		}
	}
}

func TestAgentUploadsOnlyVerifiableArtifacts(t *testing.T) {
	f := control(t, "completed")
	fake := &fakeTransport{}
	agent := f.agent(t, "completed")
	agent.Transport = fake
	workspace := filepath.Join(f.nodeDir, "workspaces", "bot")
	_ = os.MkdirAll(workspace, 0o755)
	_ = os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("hello"), 0o644)
	hello := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	agent.upload("web-1", "exec-1", "bot", []domain.Artifact{
		{Name: "a.txt", Size: 5, SHA256: hello, Reference: "a.txt"},
		{Name: "legacy.txt", Reference: "a.txt"},
		{Name: "gone.txt", Size: 5, SHA256: hello, Reference: "gone.txt"},
	})
	if len(fake.uploads) != 1 || fake.uploads[0] != "a.txt=hello" {
		t.Errorf("uploads = %v", fake.uploads)
	}
	if !strings.Contains(f.logs.String(), "artifact gone.txt not uploaded") {
		t.Errorf("logs = %s", f.logs.String())
	}
	if (Options{}).uploadTimeout() != DefaultUpload || (Options{Upload: time.Second}).uploadTimeout() != time.Second {
		t.Error("upload timeout")
	}
}
