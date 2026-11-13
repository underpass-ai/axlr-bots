package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/jev"
	"github.com/underpass-ai/axlr-bots/adapters/sqlitestore"
	"github.com/underpass-ai/axlr-bots/adapters/transcriptaudit"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// stubJudge says hidden_error to a block that holds "refused".
type stubJudge struct{ calls int }

func (s *stubJudge) Choose(_ context.Context, c transcriptaudit.Choice) (transcriptaudit.Judgement, error) {
	s.calls++
	if strings.Contains(c.State, "refused") {
		return transcriptaudit.Judgement{Choice: transcriptaudit.OptionHiddenError, Probability: 0.8}, nil
	}
	return transcriptaudit.Judgement{Choice: transcriptaudit.OptionClean, Probability: 0.2}, nil
}

func TestTranscriptAuditAgainstADaemon(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	store := sqlitestore.NewTranscriptStore(state)
	for i, content := range []string{`{"messages":[{"role":"user","content":"m"},{"role":"tool","content":"refused"}]}`, `{"messages":[{"role":"user","content":"m"},{"role":"tool","content":"fine"}]}`} {
		sum := sha256.Sum256([]byte(content))
		record := application.TranscriptRecord{Execution: "exec-1", Task: "task-1", Bot: "dev-go", Session: []string{"s1", "s2"}[i], SHA256: hex.EncodeToString(sum[:]), ReceivedAt: time.Now()}
		if _, err := store.SaveTranscript(context.Background(), record, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = store.Close()
	c := cli{t: t, state: state, axlr: "/nonexistent"}
	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	daemonDone := make(chan int, 1)
	var daemonOut, daemonErr syncBuffer
	go func() {
		daemonDone <- serve(daemonCtx, mustApp(t, state, c.axlr), state, socket, nil, &daemonOut, &daemonErr)
	}()
	defer func() { stopDaemon(); <-daemonDone }()
	for !unixapi.Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	judge := &stubJudge{}
	factory := func(model string) (transcriptaudit.Judge, string, error) { return judge, model, nil }
	storeDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"--team", "qa", "--event", "transcripcion.fallo", "--store", storeDir, "--once", "--dry-run"}
	if code := transcriptAuditServe(context.Background(), state, socket, remoteControl{}, args, factory, &stdout, &stderr); code != exitOK || strings.TrimSpace(stdout.String()) != "cursor 2" {
		t.Fatalf("dry run: %d %s %s", code, stdout.String(), stderr.String())
	}
	verdicts, _ := os.ReadFile(filepath.Join(storeDir, transcriptaudit.VerdictFileName))
	if lines := strings.Split(strings.TrimSpace(string(verdicts)), "\n"); len(lines) != 2 || !strings.Contains(lines[0], `"verdict":"hidden_error"`) || !strings.Contains(lines[1], `"verdict":"clean"`) {
		t.Fatalf("verdicts = %s", verdicts)
	}
	// The cursor is kept: a second round asks nothing.
	stdout.Reset()
	if code := transcriptAuditServe(context.Background(), state, socket, remoteControl{}, args, factory, &stdout, &stderr); code != exitOK || judge.calls != 2 {
		t.Fatalf("second: %d calls %d", code, judge.calls)
	}
	// Publishing to a team that does not exist is refused by the daemon:
	// the round stops before the verdict, so the cursor does not move.
	other := t.TempDir()
	publish := []string{"--team", "qa", "--event", "transcripcion.fallo", "--store", other, "--once"}
	if code := transcriptAuditServe(context.Background(), state, socket, remoteControl{}, publish, factory, &stdout, &stderr); code != exitError {
		t.Errorf("publish to no team: %d %s", code, stderr.String())
	}
}

func TestTranscriptAuditRefuses(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	locked := filepath.Join(t.TempDir(), "locked")
	release, err := statelock.Acquire(locked)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bad := filepath.Join(t.TempDir(), "bad")
	_ = os.MkdirAll(filepath.Join(bad, transcriptaudit.VerdictFileName), 0o700)
	ok := func(model string) (transcriptaudit.Judge, string, error) { return &stubJudge{}, model, nil }
	need := []string{"--team", "qa", "--event", "transcripcion.fallo"}
	for name, c := range map[string]struct {
		socket string
		args   []string
		judge  judgeFactory
		code   int
		want   string
	}{
		"extra":     {socket, append([]string{"x"}, need...), ok, exitUsage, "takes no arguments"},
		"no team":   {socket, []string{"--event", "e"}, ok, exitUsage, "needs --team"},
		"bad flag":  {socket, []string{"--bogus"}, ok, exitUsage, ""},
		"no socket": {"", need, ok, exitError, "--socket is empty"},
		"no key":    {socket, need, jevJudgeFor(""), exitError, "TYPESAFE_API_KEY"},
		"locked":    {socket, append([]string{"--store", locked}, need...), ok, exitError, "in use"},
		"bad log":   {socket, append([]string{"--store", bad}, need...), ok, exitError, "transcriptaudit"},
		"bad team":  {socket, []string{"--store", t.TempDir(), "--team", "Bad Team", "--event", "e"}, ok, exitError, "team"},
		"bad store": {socket, append([]string{"--store", "/proc/no/such"}, need...), ok, exitError, ""},
	} {
		var stdout, stderr bytes.Buffer
		code := transcriptAuditServe(context.Background(), state, c.socket, remoteControl{}, c.args, c.judge, &stdout, &stderr)
		if code != c.code || !strings.Contains(stderr.String(), c.want) {
			t.Errorf("%s: exit %d, %s", name, code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--state", state, "--socket", socket, "transcript-audit"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "needs serve") {
		t.Errorf("no verb: %d %s", code, stderr.String())
	}
	if judge, model, err := jevJudgeFor("k")(""); err != nil || judge == nil || model != jev.DefaultModel {
		t.Errorf("jev factory: %v %v %s", judge, err, model)
	}
}

// fixedJev is a jev.Judge with a set answer.
type fixedJev struct {
	verdict jev.Verdict
	err     error
}

func (f fixedJev) Choose(context.Context, jev.Question) (jev.Verdict, error) { return f.verdict, f.err }

func TestJevJudgeAnswersTheProbabilityOfHiddenError(t *testing.T) {
	ctx := context.Background()
	if got, _ := (jevJudge{client: fixedJev{verdict: jev.Verdict{Choice: "clean", Probability: 0.7, InputTokens: 5}}}).Choose(ctx, transcriptaudit.Choice{}); got.Choice != "clean" || got.Probability < 0.299 || got.Probability > 0.301 || got.InputTokens != 5 {
		t.Errorf("clean = %+v", got)
	}
	if got, _ := (jevJudge{client: fixedJev{verdict: jev.Verdict{Choice: "hidden_error", Probability: 0.9}}}).Choose(ctx, transcriptaudit.Choice{}); got.Probability != 0.9 {
		t.Errorf("hidden = %+v", got)
	}
	if _, err := (jevJudge{client: fixedJev{err: errors.New("down")}}).Choose(ctx, transcriptaudit.Choice{}); err == nil {
		t.Error("an error was lost")
	}
}
