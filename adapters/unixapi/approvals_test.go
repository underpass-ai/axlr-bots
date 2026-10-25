package unixapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestApproveAndDenyRoutesNameTheCallAndRefuseALateDecision(t *testing.T) {
	clock := &testClock{now: time.Now()}
	client, _, _ := serveWith(t, "approval-flow", func(d *application.Dependencies) { d.Clock = clock })
	ctx := context.Background()
	bot := definition("bot-b", "act-with-approval")
	bot.Approvals = domain.ApprovalsSnapshot{TTL: "1m"}
	if _, err := client.RegisterBot(ctx, bot); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-b"); err != nil {
		t.Fatal(err)
	}
	waiting := func(key string) string {
		submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-b", Objective: "Restart demo-broken.service", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		run, err := client.RunTask(ctx, submitted.Task.ID)
		if err != nil || run.Task.Status != "WaitingApproval" || run.Task.Approval == nil || run.Task.Approval.CallID != "c1" {
			t.Fatalf("run: %v %+v", err, run.Task)
		}
		return submitted.Task.ID
	}
	first := waiting("k-1")
	status, body, err := client.do(ctx, http.MethodPost, "/v1/tasks/"+first+"/approve?call=c9", nil)
	if err != nil || status != http.StatusConflict || !strings.Contains(string(body), `"kind":"state"`) {
		t.Errorf("another call: %d %s %v", status, body, err)
	}
	status, body, err = client.do(ctx, http.MethodPost, "/v1/tasks/"+first+"/deny?call=c1", nil)
	if err != nil || status != http.StatusOK || !strings.Contains(string(body), `"status":"Completed"`) {
		t.Fatalf("deny: %d %s %v", status, body, err)
	}
	second := waiting("k-2")
	clock.advance(time.Minute + time.Second)
	status, body, err = client.do(ctx, http.MethodPost, "/v1/tasks/"+second+"/approve?call=c1", nil)
	var response ErrorResponse
	_ = json.Unmarshal(body, &response)
	if err != nil || status != http.StatusConflict || response.Kind != KindApprovalExpired || !strings.Contains(response.Message, "authorises nothing") {
		t.Errorf("late approve: %d %s %v", status, body, err)
	}
	_, err = client.DecideTask(ctx, second, DecideRequest{Decision: "approve", CallID: "c1"})
	var remote RemoteError
	if !errors.As(err, &remote) || remote.Kind != KindApprovalExpired {
		t.Errorf("late decide: %v", err)
	}
	if task, _ := client.GetTask(ctx, second); task.Status != "WaitingApproval" {
		t.Errorf("a late decision changed the task: %+v", task)
	}
}

func digestOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestANodeUploadsArtifactsOverTLSAndTheOperatorFetchesThem(t *testing.T) {
	f := serveFleet(t)
	ctx := context.Background()
	_, node := f.enroll(t, f.token(t, "web-1", nil))
	_, other := f.enroll(t, f.token(t, "db-1", nil))
	for _, n := range []struct {
		id     string
		client *Client
	}{{"web-1", node}, {"db-1", other}} {
		if _, err := n.client.Register(ctx, application.NodeRegistration{HostName: n.id}); err != nil {
			t.Fatal(err)
		}
	}
	placed := definition("bot-a", "observe")
	placed.Placement = domain.PlacementSnapshot{Nodes: []string{"web-1"}}
	if _, err := f.operator.RegisterBot(ctx, placed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.operator.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.operator.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "write notes", IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := node.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	content := "# notes\n\nThe disk is full.\n"
	notes := domain.Artifact{Name: "notes.md", MediaType: "text/markdown", Size: int64(len(content)), SHA256: digestOf(content), Reference: ".axlr/deliverables/s/notes.md", Description: "What I found\n(line two)", Source: "inline"}
	var remote RemoteError
	if err := other.UploadArtifact(ctx, "db-1", assignment.ExecutionID, notes, strings.NewReader(content)); !errors.As(err, &remote) || remote.Kind != KindState {
		t.Errorf("another node's upload: %v", err)
	}
	if err := node.UploadArtifact(ctx, "web-1", assignment.ExecutionID, notes, strings.NewReader("# notes\n\nThe disk is FULL.\n")); !errors.As(err, &remote) || remote.Kind != KindValidation || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("bytes that do not hash: %v", err)
	}
	if err := node.UploadArtifact(ctx, "web-1", assignment.ExecutionID, notes, strings.NewReader(content)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	empty := domain.Artifact{Name: "empty.txt", MediaType: "text/plain", Size: 0, SHA256: digestOf("")}
	if err := node.UploadArtifact(ctx, "web-1", assignment.ExecutionID, empty, strings.NewReader("")); err != nil {
		t.Errorf("an empty artifact: %v", err)
	}
	// An upload that does not say its size is refused.
	request, _ := node.request(ctx, http.MethodPut, "/v1/nodes/web-1/executions/"+assignment.ExecutionID.String()+"/artifacts/x.txt", io.NopCloser(strings.NewReader("x")))
	request.ContentLength = -1
	request.Header.Set(nodeproto.ArtifactSHA256Header, digestOf("x"))
	if response, err := node.http.Do(request); err != nil || response.StatusCode != http.StatusBadRequest {
		t.Errorf("chunked upload: %v %v", err, response)
	} else {
		_ = response.Body.Close()
	}
	report := application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok", Artifacts: []domain.Artifact{notes, empty}},
		Receipt: json.RawMessage(`{"schema":"axlr.execution-receipt/v1","complete":true}`)}
	if ack, err := node.Report(ctx, "web-1", assignment.ExecutionID, report); err != nil || !ack.Accepted {
		t.Fatalf("report: %v %+v", err, ack)
	}
	execution, err := f.operator.GetExecution(ctx, assignment.ExecutionID.String())
	if err != nil || len(execution.Result.Artifacts) != 3 || !execution.Result.Artifacts[0].Stored || !execution.Result.Artifacts[1].Stored || string(execution.Receipt) != string(report.Receipt) {
		t.Fatalf("execution = %+v %v", execution, err)
	}
	if err := node.UploadArtifact(ctx, "web-1", assignment.ExecutionID, notes, strings.NewReader(content)); !errors.As(err, &remote) || remote.Kind != KindState {
		t.Errorf("an upload after the report: %v", err)
	}
	fetched, err := f.operator.GetArtifact(ctx, assignment.ExecutionID.String(), "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(fetched.Body)
	_ = fetched.Body.Close()
	if string(data) != content || fetched.Artifact.MediaType != "text/markdown" || fetched.Artifact.SHA256 != notes.SHA256 || fetched.Artifact.Size != notes.Size {
		t.Errorf("fetched %q %+v", data, fetched.Artifact)
	}
	request, _ = f.operator.request(ctx, http.MethodGet, "/v1/executions/"+assignment.ExecutionID.String()+"/artifacts/notes.md", nil)
	response, err := f.operator.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.Header.Get("Content-Disposition") != `attachment; filename="notes.md"` || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", response.Header)
	}
	if _, err := f.operator.GetArtifact(ctx, assignment.ExecutionID.String(), "other.md"); !domain.IsNotFound(err) {
		t.Errorf("an artifact that is not kept: %v", err)
	}
	if _, err := f.operator.GetArtifact(ctx, "Bad Id", "notes.md"); err == nil {
		t.Error("a bad execution id was served")
	}
	// The artifact routes are an operator's: the node address has none.
	request, _ = node.request(ctx, http.MethodGet, "/v1/executions/"+assignment.ExecutionID.String()+"/artifacts/notes.md", nil)
	if response, err := node.http.Do(request); err != nil || response.StatusCode != http.StatusNotFound {
		t.Errorf("artifact over the node address: %v %v", err, response)
	} else {
		_ = response.Body.Close()
	}
	if headerSafe("a\tb\x01c "+strings.Repeat("x", 400)) == "" || len(headerSafe(strings.Repeat("x", 400))) != 300 {
		t.Error("headerSafe")
	}
}

func TestUploadValidation(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	for _, path := range []string{"/v1/nodes/Bad/executions/exec-1/artifacts/a.txt", "/v1/nodes/web-1/executions/Bad%20Id/artifacts/a.txt"} {
		request, _ := client.request(ctx, http.MethodPut, path, strings.NewReader("x"))
		response, err := client.http.Do(request)
		if err != nil || response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %v %v", path, err, response)
			continue
		}
		_ = response.Body.Close()
	}
	request, _ := client.request(ctx, http.MethodPut, "/v1/nodes/web-1/executions/exec-1/artifacts/a.txt", strings.NewReader("x"))
	request.ContentLength = domain.MaxArtifactBytes + 1
	request.Body = io.NopCloser(io.LimitReader(zeros{}, domain.MaxArtifactBytes+1))
	response, err := client.http.Do(request)
	if err != nil || response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %v %v", err, response)
	} else {
		_ = response.Body.Close()
	}
	if err := client.UploadArtifact(ctx, "web-1", "exec-404", domain.Artifact{Name: "a.txt", Size: 1, SHA256: digestOf("x")}, strings.NewReader("x")); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	closed := NewClient("/nonexistent/cp.sock")
	if err := closed.UploadArtifact(ctx, "web-1", "exec-1", domain.Artifact{Name: "a.txt", Size: 1}, strings.NewReader("x")); err == nil {
		t.Error("uploaded to nothing")
	}
	if _, err := closed.GetArtifact(ctx, "exec-1", "a.txt"); err == nil {
		t.Error("fetched from nothing")
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
