package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/compose"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestExecutionTranscriptPrintsTheWholeSession(t *testing.T) {
	state := t.TempDir()
	session := `{"version":3,"messages":[{"role":"user","content":"go"},{"role":"tool", "content":"` + strings.Repeat("q", 40000) + `"}]}`
	store := compose.TranscriptStore(state)
	record := application.TranscriptRecord{Execution: "exec-1", Task: "task-1", Bot: "worker-1", Session: "sess-1", Size: int64(len(session)), SHA256: "x", ReceivedAt: time.Now()}
	if _, err := store.SaveTranscript(context.Background(), record, []byte(session)); err != nil {
		t.Fatal(err)
	}
	c := cli{t: t, state: state, axlr: "/nonexistent"}
	if code, out, _ := c.run("execution", "transcript", "exec-1", "sess-1"); code != exitOK || out != session {
		t.Fatalf("raw: %d %d bytes", code, len(out))
	}
	code, out, _ := c.run("execution", "transcript", "exec-1", "sess-1", "--jsonl")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != exitOK || len(lines) != 2 || lines[0] != `{"role":"user","content":"go"}` || !strings.HasPrefix(lines[1], `{"role":"tool","content":"qqq`) {
		t.Fatalf("jsonl: %d %q", code, out[:min(len(out), 200)])
	}
	file := filepath.Join(t.TempDir(), "t.json")
	if code, _, _ := c.run("execution", "transcript", "--out", file, "exec-1", "sess-1"); code != exitOK {
		t.Fatal("out")
	}
	if data, _ := os.ReadFile(file); string(data) != session {
		t.Errorf("file holds %d bytes", len(data))
	}
	if code, out, _ := c.run("execution", "transcripts"); code != exitOK || !strings.Contains(out, `"session": "sess-1"`) {
		t.Errorf("list: %d %s", code, out)
	}
	if code, out, _ := c.run("execution", "transcripts", "--after", "1"); code != exitOK || strings.TrimSpace(out) != "[]" {
		t.Errorf("list after: %d %s", code, out)
	}
	for _, usage := range [][]string{{"execution", "transcript", "exec-1"}, {"execution", "transcript", "--nope"}, {"execution", "transcripts", "x"}, {"execution", "transcripts", "--nope"}} {
		if code, _, _ := c.run(usage...); code != exitUsage {
			t.Errorf("%v: %d", usage, code)
		}
	}
	if code, _, _ := c.run("execution", "transcript", "exec-1", "sess-9"); code != exitError {
		t.Errorf("missing: %d", code)
	}
	if code, _, _ := c.run("execution", "transcript", "--out", filepath.Join(t.TempDir(), "no", "x"), "exec-1", "sess-1"); code != exitError {
		t.Errorf("bad out: %d", code)
	}
	if code, _, _ := c.run("execution", "transcripts", "--after", "-1"); code != exitError {
		t.Errorf("negative cursor: %d", code)
	}
	if _, err := jsonLines([]byte("not json")); err == nil {
		t.Error("jsonl of not a session")
	}
	if _, err := jsonLines([]byte(`{"messages":[{"a":}]}`)); err == nil {
		t.Error("jsonl of a broken message")
	}
}
