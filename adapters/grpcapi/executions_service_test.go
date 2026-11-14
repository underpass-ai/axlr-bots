package grpcapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// runningExecution is an execution a node holds, so that it can upload.
func runningExecution(t *testing.T, f *fixture) domain.ExecutionID {
	t.Helper()
	ctx := context.Background()
	registerNode(t, f, "web-1")
	registerAndEnable(t, f, "bot-a", "observe")
	if _, err := operatorv1.NewTasksServiceClient(f.authed).SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "write notes", IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := f.local.Runner().NextAssignment(ctx, "web-1")
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	return assignment.ExecutionID
}

func TestExecutionsServiceStreamsAnArtifactInChunks(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	exec := runningExecution(t, f)
	content := strings.Repeat("0123456789abcdef", 10_000) // 160,000 bytes: three chunks
	notes := domain.Artifact{Name: "notes.md", MediaType: "text/markdown", Size: int64(len(content)), SHA256: digest(content), Reference: ".axlr/notes.md", Description: "What I found", Source: "inline"}
	if _, err := application.NewUploadArtifact(f.deps).Execute(ctx, "web-1", exec, notes, strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	report := application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok", Artifacts: []domain.Artifact{notes}},
		Receipt: []byte(`{"schema":"axlr.execution-receipt/v1","complete":true}`)}
	if ack, err := f.local.Runner().Report(ctx, "web-1", exec, report); err != nil || !ack.Accepted {
		t.Fatalf("report: %v %+v", err, ack)
	}
	client := operatorv1.NewExecutionsServiceClient(f.authed)
	got1, err := client.GetExecution(ctx, &operatorv1.GetExecutionRequest{ExecutionId: exec.String()})
	if err != nil || !got1.GetExecution().GetResult().GetArtifacts()[0].GetStored() || !strings.Contains(string(got1.GetExecution().GetReceipt()), "execution-receipt") || got1.GetExecution().GetNodeId() != "web-1" {
		t.Fatalf("execution: %v %v", got1, err)
	}
	stream, err := client.GetArtifact(ctx, &operatorv1.GetArtifactRequest{ExecutionId: exec.String(), Name: "notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetArtifact().GetName() != "notes.md" || first.GetArtifact().GetSize() != int64(len(content)) || first.GetArtifact().GetSha256() != digest(content) || first.GetArtifact().GetMediaType() != "text/markdown" {
		t.Fatalf("first message: %v %v", first, err)
	}
	var got bytes.Buffer
	chunks := 0
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(message.GetChunk()) == 0 || len(message.GetChunk()) > 64<<10 {
			t.Fatalf("a chunk of %d bytes", len(message.GetChunk()))
		}
		chunks++
		got.Write(message.GetChunk())
	}
	if got.String() != content || chunks != 3 {
		t.Errorf("content: %d bytes in %d chunks", got.Len(), chunks)
	}

	for _, request := range []*operatorv1.GetArtifactRequest{
		{ExecutionId: exec.String(), Name: "missing.md"},
		{ExecutionId: "exec-nope", Name: "notes.md"},
		{ExecutionId: exec.String(), Name: "../escape"},
	} {
		stream, _ := client.GetArtifact(ctx, request)
		if _, err := stream.Recv(); err == nil {
			t.Errorf("artifact %+v was served", request)
		}
	}
}

func TestExecutionsServiceListsAndStreamsTranscripts(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	exec := runningExecution(t, f)
	content := `{"messages":[{"role":"tool","content":"` + strings.Repeat("z", 100_000) + `"}]}`
	uploader := application.NewUploadTranscript(f.deps)
	if _, err := uploader.Execute(ctx, "web-1", exec, application.TranscriptUpload{Session: "sess-1", Content: []byte(content), SHA256: digest(content)}); err != nil {
		t.Fatal(err)
	}
	if _, err := uploader.Execute(ctx, "web-1", exec, application.TranscriptUpload{Session: "sess-2", Content: nil, SHA256: digest("")}); err != nil {
		t.Fatal(err)
	}
	client := operatorv1.NewExecutionsServiceClient(f.authed)

	list, err := client.ListTranscripts(ctx, &operatorv1.ListTranscriptsRequest{})
	if err != nil || len(list.GetRecords()) != 2 || list.GetRecords()[0].GetSession() != "sess-1" || list.GetRecords()[0].GetSize() != int64(len(content)) || list.GetRecords()[0].GetReceivedAt() == nil {
		t.Fatalf("list: %v %v", list, err)
	}
	if rest, err := client.ListTranscripts(ctx, &operatorv1.ListTranscriptsRequest{After: list.GetRecords()[0].GetSeq(), Limit: 5}); err != nil || len(rest.GetRecords()) != 1 || rest.GetRecords()[0].GetSession() != "sess-2" {
		t.Errorf("after the first: %v %v", rest, err)
	}
	for _, request := range []*operatorv1.ListTranscriptsRequest{{Limit: -1}, {After: -1}} {
		if _, err := client.ListTranscripts(ctx, request); err == nil {
			t.Errorf("%+v was taken", request)
		}
	}

	stream, err := client.GetTranscript(ctx, &operatorv1.GetTranscriptRequest{ExecutionId: exec.String(), Session: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetTranscript().GetSession() != "sess-1" || first.GetTranscript().GetSha256() != digest(content) {
		t.Fatalf("first message: %v %v", first, err)
	}
	var got bytes.Buffer
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got.Write(message.GetChunk())
	}
	if got.String() != content {
		t.Errorf("transcript: %d bytes, want %d", got.Len(), len(content))
	}
	empty, _ := client.GetTranscript(ctx, &operatorv1.GetTranscriptRequest{ExecutionId: exec.String(), Session: "sess-2"})
	if message, err := empty.Recv(); err != nil || message.GetTranscript() == nil {
		t.Fatalf("empty transcript: %v %v", message, err)
	}
	if _, err := empty.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("an empty transcript has no chunks: %v", err)
	}
	for _, request := range []*operatorv1.GetTranscriptRequest{
		{ExecutionId: exec.String(), Session: "sess-9"},
		{ExecutionId: "Bad Id", Session: "sess-1"},
	} {
		stream, _ := client.GetTranscript(ctx, request)
		if _, err := stream.Recv(); err == nil {
			t.Errorf("transcript %+v was served", request)
		}
	}
}
