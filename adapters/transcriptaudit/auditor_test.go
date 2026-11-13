package transcriptaudit

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var errBoom = errors.New("boom")

type fakeSource struct {
	records  []application.TranscriptRecord
	content  map[string][]byte
	listErr  error
	getErr   error
	groupErr error
}

func (f *fakeSource) ListGroups(context.Context) ([]application.Group, error) {
	if f.groupErr != nil {
		return nil, f.groupErr
	}
	return []application.Group{
		{Name: "qa", Members: []application.GroupMember{{BotID: "qa-1"}, {BotID: "both"}}},
		{Name: "dev", Members: []application.GroupMember{{BotID: "dev-go"}, {BotID: "both"}}},
	}, nil
}

func (f *fakeSource) ListTranscripts(_ context.Context, after int64, limit int) ([]application.TranscriptRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []application.TranscriptRecord
	for _, r := range f.records {
		if r.Seq > after && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeSource) GetTranscript(_ context.Context, execution, session string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.content[execution+"/"+session], nil
}

// fakeJudge says hidden_error to any block holding "ok\": false".
type fakeJudge struct {
	calls int
	err   error
	sizes []int
}

func (j *fakeJudge) Choose(_ context.Context, c Choice) (Judgement, error) {
	j.calls++
	j.sizes = append(j.sizes, len(c.State))
	if j.err != nil {
		return Judgement{}, j.err
	}
	if c.Question != question || len(c.Options) != 2 {
		return Judgement{}, errors.New("bad choice")
	}
	if strings.Contains(c.State, `ok\\\": false`) || strings.Contains(c.State, `\"ok\": false`) {
		return Judgement{Choice: OptionHiddenError, Probability: 0.9, InputTokens: 10}, nil
	}
	return Judgement{Choice: OptionClean, Probability: 0.1, InputTokens: 10}, nil
}

type fakePublisher struct {
	commands []application.PublishGroupEventCommand
	err      error
}

func (p *fakePublisher) PublishGroupEvent(_ context.Context, c application.PublishGroupEventCommand) (application.PublishAnswer, error) {
	if p.err != nil {
		return application.PublishAnswer{}, p.err
	}
	p.commands = append(p.commands, c)
	return application.PublishAnswer{EventID: "ev-" + c.IdempotencyKey}, nil
}

func session(results ...string) []byte {
	var b strings.Builder
	b.WriteString(`{"messages":[{"role":"user","content":"mission: if it fails say \"ok\": false"}`)
	for i, r := range results {
		id := "c" + string(rune('a'+i))
		b.WriteString(`,{"role":"assistant","tool_calls":[{"id":"` + id + `","name":"local_exec","arguments":{"program":"/b/arreglo","args":["plan"]}}]}`)
		b.WriteString(`,{"role":"tool","tool_call_id":"` + id + `","content":` + r + `}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func rec(seq int64, bot string) application.TranscriptRecord {
	return application.TranscriptRecord{Seq: seq, Execution: domain.ExecutionID("exec-" + bot), Task: domain.TaskID("task-" + bot), Bot: domain.BotID(bot), Session: "s"}
}

func newTestAuditor(t *testing.T, source *fakeSource, judge *fakeJudge, publisher Publisher) (*Auditor, *VerdictLog, *bytes.Buffer) {
	t.Helper()
	vl, err := OpenVerdictLog(filepath.Join(t.TempDir(), VerdictFileName))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	a, err := NewAuditor(AuditorConfig{Source: source, Publisher: publisher, Judge: judge, Log: vl, Team: "qa", Event: "transcripcion.fallo", Model: "jev-1.13.0", Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }, Logger: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	return a, vl, &logs
}

func TestTheAuditorPublishesTranscriptsThatHideAFailure(t *testing.T) {
	big := `"` + strings.Repeat("x", 60000) + `"`
	source := &fakeSource{
		records: []application.TranscriptRecord{rec(1, "dev-go"), rec(2, "qa-1"), rec(3, "nobody"), rec(4, "both")},
		content: map[string][]byte{
			"exec-dev-go/s": session(`"{\"ok\": false, \"error\": \"files is a non-empty list\"}"`, big),
			"exec-qa-1/s":   session(`"{\"ok\": true}"`),
			"exec-nobody/s": []byte("not a session"),
			"exec-both/s":   session(`"{\"ok\": false}"`),
		},
	}
	judge := &fakeJudge{}
	publisher := &fakePublisher{}
	a, vl, logs := newTestAuditor(t, source, judge, publisher)
	audited, err := a.Round(context.Background())
	if err != nil || audited != 4 || vl.Cursor() != 4 {
		t.Fatalf("round: %d %v cursor %d", audited, err, vl.Cursor())
	}
	for _, size := range judge.sizes {
		if size > MaxStateBytes {
			t.Errorf("a state of %d bytes", size)
		}
	}
	if len(publisher.commands) != 2 {
		t.Fatalf("published %d", len(publisher.commands))
	}
	first := publisher.commands[0]
	if first.Group != "qa" || first.Name != "transcripcion.fallo" || first.IdempotencyKey != "transcript:1" || !strings.Contains(first.Message, "task-dev-go · dev-go (dev) · 1 of ") || first.Data["session"] != "s" {
		t.Errorf("first = %+v", first)
	}
	if !strings.Contains(publisher.commands[1].Message, "both (dev)") {
		t.Errorf("a bot in two teams is named by the first: %s", publisher.commands[1].Message)
	}
	if !strings.Contains(logs.String(), "3 skipped") {
		t.Errorf("logs = %s", logs.String())
	}
	// Nothing new: a round audits nothing.
	if audited, err := a.Round(context.Background()); err != nil || audited != 0 {
		t.Errorf("second round: %d %v", audited, err)
	}
	// A log reopened keeps the cursor.
	again, _ := OpenVerdictLog(vl.path)
	if again.Cursor() != 4 {
		t.Errorf("reopened cursor %d", again.Cursor())
	}
}

func TestAnAuditThatFailsStopsTheRoundAndRetries(t *testing.T) {
	source := &fakeSource{records: []application.TranscriptRecord{rec(1, "dev-go")}, content: map[string][]byte{"exec-dev-go/s": session(`"{\"ok\": false}"`)}}
	judge := &fakeJudge{err: errBoom}
	publisher := &fakePublisher{}
	a, vl, _ := newTestAuditor(t, source, judge, publisher)
	if _, err := a.Round(context.Background()); !errors.Is(err, errBoom) || vl.Cursor() != 0 {
		t.Fatalf("judge failure: %v %d", err, vl.Cursor())
	}
	judge.err = nil
	publisher.err = errBoom
	if _, err := a.Round(context.Background()); !errors.Is(err, errBoom) || vl.Cursor() != 0 {
		t.Fatalf("publish failure: %v %d", err, vl.Cursor())
	}
	publisher.err = nil
	source.getErr = errBoom
	if _, err := a.Round(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("get failure: %v", err)
	}
	source.getErr = nil
	source.listErr = errBoom
	if _, err := a.Round(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("list failure: %v", err)
	}
	source.listErr = nil
	source.groupErr = errBoom
	if audited, err := a.Round(context.Background()); err != nil || audited != 1 || len(publisher.commands) != 1 {
		t.Fatalf("retry: %d %v", audited, err)
	}
	// The teams of an earlier round are kept when the list fails.
	if !strings.Contains(publisher.commands[0].Message, "dev-go (dev)") {
		t.Errorf("kept teams: %s", publisher.commands[0].Message)
	}
}

func TestADryRunRecordsAndPublishesNothing(t *testing.T) {
	source := &fakeSource{records: []application.TranscriptRecord{rec(1, "dev-go")}, content: map[string][]byte{"exec-dev-go/s": session(`"{\"ok\": false}"`)}, groupErr: errBoom}
	a, vl, _ := newTestAuditor(t, source, &fakeJudge{}, nil)
	if err := a.Run(context.Background(), time.Hour, true); err != nil || vl.Cursor() != 1 {
		t.Fatalf("once: %v %d", err, vl.Cursor())
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	source := &fakeSource{listErr: errBoom}
	a, _, logs := newTestAuditor(t, source, &fakeJudge{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, 5*time.Millisecond, false) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("logs = %s", logs.String())
	}
	if err := a.Run(context.Background(), time.Hour, true); !errors.Is(err, errBoom) {
		t.Errorf("once with an error: %v", err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if err := a.Run(cancelled, time.Hour, false); err != nil {
		t.Errorf("cancelled: %v", err)
	}
}

func TestNewAuditorChecksItsConfiguration(t *testing.T) {
	vl, _ := OpenVerdictLog(filepath.Join(t.TempDir(), VerdictFileName))
	good := AuditorConfig{Source: &fakeSource{}, Judge: &fakeJudge{}, Log: vl, Team: "qa", Event: "transcripcion.fallo"}
	if _, err := NewAuditor(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AuditorConfig){
		func(c *AuditorConfig) { c.Source = nil },
		func(c *AuditorConfig) { c.Judge = nil },
		func(c *AuditorConfig) { c.Log = nil },
		func(c *AuditorConfig) { c.Team = "Bad Team" },
		func(c *AuditorConfig) { c.Event = "Bad Event" },
	} {
		c := good
		change(&c)
		if _, err := NewAuditor(c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
}
