package grpcclient

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

func digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func artifactStream(name, content string, size int64, sha string, chunk int) []*operatorv1.GetArtifactResponse {
	messages := []*operatorv1.GetArtifactResponse{{Content: &operatorv1.GetArtifactResponse_Artifact{Artifact: &operatorv1.Artifact{Name: name, MediaType: "text/plain", Size: size, Sha256: sha}}}}
	for start := 0; start < len(content); start += chunk {
		end := min(start+chunk, len(content))
		messages = append(messages, &operatorv1.GetArtifactResponse{Content: &operatorv1.GetArtifactResponse_Chunk{Chunk: []byte(content[start:end])}})
	}
	return messages
}

func TestAnArtifactArrivesInChunksAndIsChecked(t *testing.T) {
	h := newHarness(t)
	content := strings.Repeat("0123456789", 100)
	h.fake.artifact = artifactStream("report.txt", content, int64(len(content)), digest(content), 64)
	got, err := h.client.GetArtifact(t.Context(), "e-1", "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Artifact.Name != "report.txt" || got.Artifact.MediaType != "text/plain" || !got.Artifact.Stored || got.Artifact.Size != int64(len(content)) {
		t.Errorf("artifact: %+v", got.Artifact)
	}
	read, err := io.ReadAll(got.Body)
	if err != nil || string(read) != content {
		t.Fatalf("body: %d bytes, %v", len(read), err)
	}
	if err := got.Body.Close(); err != nil {
		t.Error(err)
	}
	if headers := h.fake.headers("GetArtifact").Get("authorization"); len(headers) != 1 || headers[0] != "Bearer "+testToken {
		t.Errorf("a stream carries the token too: %v", headers)
	}
	if request := h.fake.request("GetArtifact").(*operatorv1.GetArtifactRequest); request.GetExecutionId() != "e-1" || request.GetName() != "report.txt" {
		t.Errorf("request: %v", request)
	}
}

func TestAnArtifactThatDoesNotMatchIsRefused(t *testing.T) {
	content := "hello world"
	for name, stream := range map[string][]*operatorv1.GetArtifactResponse{
		"short":     artifactStream("a", content, int64(len(content))+5, digest(content), 4),
		"corrupted": artifactStream("a", content, int64(len(content)), digest("other"), 4),
	} {
		h := newHarness(t)
		h.fake.artifact = stream
		got, err := h.client.GetArtifact(t.Context(), "e", "a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadAll(got.Body); err == nil {
			t.Errorf("%s: a body that does not match should fail at the end", name)
		}
		got.Body.Close()
	}
}

func TestAnArtifactWithoutItsHeaderOrWithAStreamError(t *testing.T) {
	h := newHarness(t)
	h.fake.artifact = []*operatorv1.GetArtifactResponse{{Content: &operatorv1.GetArtifactResponse_Chunk{Chunk: []byte("x")}}}
	if _, err := h.client.GetArtifact(t.Context(), "e", "a"); err == nil || !strings.Contains(err.Error(), "did not start with the artifact") {
		t.Errorf("no header: %v", err)
	}
	h = newHarness(t)
	h.fake.artifact = nil
	if _, err := h.client.GetArtifact(t.Context(), "e", "a"); err == nil {
		t.Error("an empty stream is an error")
	}
}

func TestClosingTheBodyEndsTheStream(t *testing.T) {
	h := newHarness(t)
	h.fake.cancelled = make(chan struct{})
	h.fake.artifact = artifactStream("a", "0123456789", 100, "", 5)
	got, err := h.client.GetArtifact(t.Context(), "e", "a")
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if n, err := got.Body.Read(buf); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	got.Body.Close()
	select {
	case <-h.fake.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the server never saw the stream end")
	}
}

func transcriptStream(content, sha string, size int64) []*operatorv1.GetTranscriptResponse {
	messages := []*operatorv1.GetTranscriptResponse{{Content: &operatorv1.GetTranscriptResponse_Transcript{Transcript: &operatorv1.TranscriptRecord{Seq: 1, Session: "s", Size: size, Sha256: sha}}}}
	for start := 0; start < len(content); start += 7 {
		end := min(start+7, len(content))
		messages = append(messages, &operatorv1.GetTranscriptResponse{Content: &operatorv1.GetTranscriptResponse_Chunk{Chunk: []byte(content[start:end])}})
	}
	return messages
}

func TestATranscriptIsReadWholeAndChecked(t *testing.T) {
	h := newHarness(t)
	content := `{"line":1}` + "\n" + `{"line":2}` + "\n"
	h.fake.transcript = transcriptStream(content, digest(content), int64(len(content)))
	got, err := h.client.GetTranscript(t.Context(), "e-1", "s")
	if err != nil || string(got) != content {
		t.Fatalf("transcript: %q %v", got, err)
	}
	if request := h.fake.request("GetTranscript").(*operatorv1.GetTranscriptRequest); request.GetExecutionId() != "e-1" || request.GetSession() != "s" {
		t.Errorf("request: %v", request)
	}
	h.fake.transcript = transcriptStream(content, digest("other"), int64(len(content)))
	if _, err := h.client.GetTranscript(t.Context(), "e-1", "s"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("a bad digest: %v", err)
	}
	h.fake.transcript = transcriptStream(content, digest(content), 3)
	if _, err := h.client.GetTranscript(t.Context(), "e-1", "s"); err == nil {
		t.Error("a bad size")
	}
	h.fake.transcript = []*operatorv1.GetTranscriptResponse{{Content: &operatorv1.GetTranscriptResponse_Chunk{Chunk: []byte("x")}}}
	if _, err := h.client.GetTranscript(t.Context(), "e-1", "s"); err == nil || !strings.Contains(err.Error(), "did not start with its record") {
		t.Errorf("no record: %v", err)
	}
}

func TestTranscriptRecordsAreListed(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("ExecutionsService", "ListTranscripts"), &operatorv1.ListTranscriptsResponse{Records: []*operatorv1.TranscriptRecord{
		{Seq: 7, ExecutionId: "e-1", TaskId: "t-1", BotId: "bot-a", Session: "s", Size: 10, Sha256: "ab", Filtered: true, SentSha256: "cd", ReceivedAt: ts(t0)},
	}})
	records, err := h.client.ListTranscripts(t.Context(), 6, 50)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	r := records[0]
	if r.Seq != 7 || r.Execution != "e-1" || r.Task != "t-1" || r.Bot != "bot-a" || r.Session != "s" || r.Size != 10 || r.SHA256 != "ab" || !r.Filtered || r.SentSHA256 != "cd" || !r.ReceivedAt.Equal(t0) {
		t.Errorf("record: %+v", r)
	}
	if request := h.fake.request(method("ExecutionsService", "ListTranscripts")).(*operatorv1.ListTranscriptsRequest); request.GetAfter() != 6 || request.GetLimit() != 50 {
		t.Errorf("request: %v", request)
	}
}
