package unixapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestANodeUploadsATranscriptAndTheOperatorReadsItWhole(t *testing.T) {
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
	if _, err := f.local.RegisterBot(ctx, placed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.local.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.local.SubmitTask(ctx, application.SubmitRequest{BotID: "bot-a", Objective: "write notes", IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := node.NextAssignment(ctx, "web-1", 5*time.Second)
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	exec := assignment.ExecutionID
	content := `{"messages":[{"role":"tool","content":"` + strings.Repeat("z", 100000) + `"}]}`
	good := application.TranscriptUpload{Session: "sess-1", Content: []byte(content), SHA256: digestOf(content)}
	var remote application.RemoteError
	if err := other.UploadTranscript(ctx, "db-1", exec, good); !errors.As(err, &remote) || remote.Kind != application.KindState {
		t.Errorf("another node's upload: %v", err)
	}
	wrong := good
	wrong.SHA256 = digestOf("other")
	if err := node.UploadTranscript(ctx, "web-1", exec, wrong); !errors.As(err, &remote) || remote.Kind != application.KindValidation {
		t.Errorf("bytes that do not hash: %v", err)
	}
	if err := node.UploadTranscript(ctx, "web-1", exec, good); err != nil {
		t.Fatalf("upload: %v", err)
	}
	empty := application.TranscriptUpload{Session: "sess-2", SHA256: digestOf("")}
	if err := node.UploadTranscript(ctx, "web-1", exec, empty); err != nil {
		t.Errorf("an empty transcript: %v", err)
	}
	records, err := f.local.ListTranscripts(ctx, 0, 0)
	if err != nil || len(records) != 2 || records[0].Session != "sess-1" || records[0].Bot != "bot-a" || records[0].Size != int64(len(content)) {
		t.Fatalf("records = %+v %v", records, err)
	}
	if records, err := f.local.ListTranscripts(ctx, 1, 5); err != nil || len(records) != 1 || records[0].Session != "sess-2" {
		t.Fatalf("after 1 = %+v %v", records, err)
	}
	data, err := f.local.GetTranscript(ctx, exec.String(), "sess-1")
	if err != nil || string(data) != content {
		t.Fatalf("get: %d bytes %v", len(data), err)
	}
	if _, err := f.local.GetTranscript(ctx, exec.String(), "sess-9"); !domain.IsNotFound(err) {
		t.Errorf("missing transcript: %v", err)
	}
	report := application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}
	if ack, err := node.Report(ctx, "web-1", exec, report); err != nil || !ack.Accepted {
		t.Fatalf("report: %v %+v", err, ack)
	}
	if err := node.UploadTranscript(ctx, "web-1", exec, good); !errors.As(err, &remote) || remote.Kind != application.KindState {
		t.Errorf("an upload after the report: %v", err)
	}
	for _, path := range []string{"/v1/nodes/bad%20node/executions/" + exec.String() + "/transcripts/s", "/v1/nodes/web-1/executions/bad%20exec/transcripts/s"} {
		request, _ := node.request(ctx, http.MethodPut, path, strings.NewReader("{}"))
		response, err := node.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode < 400 {
			t.Errorf("%s answered %d", path, response.StatusCode)
		}
	}
	request, _ := node.request(ctx, http.MethodPut, "/v1/nodes/web-1/executions/"+exec.String()+"/transcripts/s", strings.NewReader("{}"))
	request.ContentLength = application.MaxTranscriptBytes + 1
	if response, err := node.http.Do(request); err == nil {
		_ = response.Body.Close()
		if response.StatusCode < 400 {
			t.Errorf("an oversized upload answered %d", response.StatusCode)
		}
	}
}

func TestTranscriptClientErrors(t *testing.T) {
	client := NewClient("/nonexistent/socket")
	ctx := context.Background()
	if err := client.UploadTranscript(ctx, "web-1", "exec-1", application.TranscriptUpload{Session: "s"}); err == nil {
		t.Error("upload to nowhere")
	}
}
