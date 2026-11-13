package nodeagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// sessionState writes AXLR's session file for sess-1 under a state
// directory of the test's and returns the directory.
func sessionState(t *testing.T, content string) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "axlr")
	if err := os.MkdirAll(filepath.Join(state, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "sessions", "sess-1.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAgentUploadsTheWholeTranscriptBeforeItReports(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	agent := f.agent(t, "completed")
	session := `{"version":3,"messages":[{"role":"user","content":"go"},{"role":"tool","content":"` + strings.Repeat("x", 70000) + `"}]}`
	agent.Executor.(*axlr.Executor).SessionState = sessionState(t, session)
	agentCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	defer func() { stop(); <-done }()
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "m", AllowedTools: []string{"local_read"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, _ := f.client.SubmitTask(ctx, unixapiSubmit("bot", "do it"))
	completed := waitFor(t, f.client, submitted.Task.ID, "Completed")
	records, err := f.client.ListTranscripts(ctx, 0, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("records: %+v %v", records, err)
	}
	record := records[0]
	if record.Task.String() != completed.ID || record.Bot != "bot" || record.Session != "sess-1" || record.Size != int64(len(session)) || record.Seq != 1 {
		t.Fatalf("record = %+v", record)
	}
	content, err := f.client.GetTranscript(ctx, record.Execution.String(), "sess-1")
	if err != nil || string(content) != session {
		t.Fatalf("content %d bytes, %v", len(content), err)
	}
	if !strings.Contains(f.logs.String(), "transcript sess-1 uploaded") {
		t.Errorf("logs = %s", f.logs.String())
	}
	if more, err := f.client.ListTranscripts(ctx, 1, 10); err != nil || len(more) != 0 {
		t.Errorf("after the cursor: %+v %v", more, err)
	}
}

// noSessions is an executor that keeps no session files.
type noSessions struct{ Executor }

func TestUploadTranscriptsLogsWhatItCannotSend(t *testing.T) {
	f := control(t, "completed")
	agent := f.agent(t, "completed")
	fake := &fakeTransport{}
	agent.Transport = fake
	agent.Executor.(*axlr.Executor).SessionState = sessionState(t, `{"messages":[]}`)

	agent.uploadTranscripts("web-1", "exec-1", []string{"sess-1", "missing", "../x"})
	if len(fake.transcripts) != 1 || !strings.HasPrefix(fake.transcripts[0], `sess-1={"messages":[]}#`) {
		t.Fatalf("transcripts = %v", fake.transcripts)
	}
	for _, want := range []string{"transcript missing not read", "transcript ../x not read"} {
		if !strings.Contains(f.logs.String(), want) {
			t.Errorf("logs lack %q: %s", want, f.logs.String())
		}
	}
	fake.transcriptErr = errors.New("refused")
	agent.uploadTranscripts("web-1", "exec-1", []string{"sess-1"})
	if !strings.Contains(f.logs.String(), "transcript sess-1 not uploaded: refused") {
		t.Errorf("logs = %s", f.logs.String())
	}
	// No sessions, or a runtime that keeps none: nothing is tried.
	fake.transcriptErr = nil
	agent.uploadTranscripts("web-1", "exec-1", nil)
	agent.Executor = noSessions{agent.Executor}
	agent.uploadTranscripts("web-1", "exec-1", []string{"sess-1"})
	if len(fake.transcripts) != 1 {
		t.Errorf("transcripts = %v", fake.transcripts)
	}
}

func TestRuntimesReadSessionFromAXLROnly(t *testing.T) {
	executor := axlr.NewExecutor("/nonexistent", t.TempDir())
	executor.SessionState = sessionState(t, `{"m":1}`)
	runtimes := NewRuntimes(executor)
	if content, err := runtimes.ReadSession("sess-1"); err != nil || string(content) != `{"m":1}` {
		t.Fatalf("read: %q %v", content, err)
	}
	if _, err := NewRuntimes(noSessions{executor}).ReadSession("sess-1"); err == nil {
		t.Error("a runtime without sessions read one")
	}
	var _ domain.NodeID = "web-1"
}
