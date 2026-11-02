package nodeagent

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

// A run's tool calls and words reach the task's timeline while it runs,
// in order, and before its report.
func TestTheRunsProgressReachesTheTasksTimeline(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	bin, _, err := fakeaxlr.Write(f.dir, fakeaxlr.Options{Scenario: "progress"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{Transport: f.client, Executor: axlr.NewExecutor(bin, f.nodeDir), Options: Options{NodeID: "web-1", HostName: "web-1", Heartbeat: 50 * time.Millisecond, PollWait: 100 * time.Millisecond, Renew: 30 * time.Millisecond, ProgressEvery: 50 * time.Millisecond, Log: log.New(f.logs, "", 0)}}
	agentCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = agent.Run(agentCtx) }()
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "watch the pods", AllowedTools: []string{"local_read"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := f.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "are the pods up?", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, f.client, submitted.Task.ID, "Completed")
	timeline, err := f.client.ListTaskEvents(ctx, application.TaskEventsQuery{ID: domain.TaskID(submitted.Task.ID)})
	if err != nil {
		t.Fatal(err)
	}
	var kinds, words []string
	reported := -1
	for i, event := range timeline {
		switch event.Type {
		case domain.EventExecutionReported:
			reported = i
		case domain.EventTaskProgress:
			if reported >= 0 {
				t.Fatalf("progress after the report: %+v", timeline)
			}
			for _, item := range event.Payload["items"].([]any) {
				fields := item.(map[string]any)
				kinds = append(kinds, fields["kind"].(string))
				if fields["kind"] == "text" {
					words = append(words, fields["text"].(string))
				}
				if fields["kind"] == "tool.started" && fields["tool"] == "axlr_call_tool" && fields["mcpTool"] != "list_pods" {
					t.Errorf("the bridged call names its MCP tool: %+v", fields)
				}
				if fields["kind"] == "tool.ended" && fields["callId"] == "c1" && fields["durationMs"] == nil {
					t.Errorf("a call that took a while says how long: %+v", fields)
				}
			}
		}
	}
	if got := strings.Join(kinds, ","); got != "text,tool.started,tool.ended,tool.started,tool.ended,text" {
		t.Fatalf("kinds = %s\n%s", got, f.logs.String())
	}
	if strings.Join(words, "|") != "Looking at the pods.|All pods run." {
		t.Fatalf("words = %q", words)
	}
}

// progressTransport answers progress batches as a test asks.
type progressTransport struct {
	fakeTransport
	mu      sync.Mutex
	batches []application.ProgressBatch
	answer  func(n int) (application.ProgressAnswer, error)
}

func (p *progressTransport) Progress(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, batch application.ProgressBatch) (application.ProgressAnswer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batches = append(p.batches, batch)
	if p.answer == nil {
		return application.ProgressAnswer{Recorded: true}, nil
	}
	return p.answer(len(p.batches))
}

func (p *progressTransport) sent() []application.ProgressBatch {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]application.ProgressBatch(nil), p.batches...)
}

func sender(transport application.NodeTransport, logs *syncBuffer, options Options) *progressSender {
	options.Log = log.New(logs, "", 0)
	return Agent{Transport: transport, Options: options}.startProgress(context.Background(), "web-1", "exec-1")
}

func text(words string) application.ProgressItem {
	return application.ProgressItem{Kind: application.ProgressText, Text: words}
}

// Words wait for more words or for their time; a call goes at once.
func TestWordsAreCoalescedAndACallGoesAtOnce(t *testing.T) {
	transport := &progressTransport{}
	s := sender(transport, &syncBuffer{}, Options{ProgressEvery: 300 * time.Millisecond, ProgressChars: 20})
	s.push(text("Looking "))
	s.push(text("at the pods"))
	time.Sleep(100 * time.Millisecond)
	if len(transport.sent()) != 0 {
		t.Fatal("a few words wait for more")
	}
	time.Sleep(400 * time.Millisecond)
	if sent := transport.sent(); len(sent) != 1 || len(sent[0].Items) != 1 || sent[0].Items[0].Text != "Looking at the pods" || sent[0].Items[0].At.IsZero() {
		t.Fatalf("their time came: %+v", sent)
	}
	s.push(text(strings.Repeat("w", 25)))
	s.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "local_read"})
	deadline := time.Now().Add(2 * time.Second)
	for len(transport.sent()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sent := transport.sent(); len(sent) != 2 || len(sent[1].Items) != 2 {
		t.Fatalf("enough words and a call go at once: %+v", sent)
	}
	s.push(text("last words"))
	s.finish()
	if sent := transport.sent(); len(sent) != 3 || sent[2].Items[0].Text != "last words" {
		t.Fatalf("finish sends what is left: %+v", sent)
	}
}

// A queue past its limit drops the oldest and says so; the words of one
// item stay within an item's bound.
func TestTheQueueDropsTheOldest(t *testing.T) {
	q := newProgressQueue(2)
	now := time.Now()
	if send, after := q.due(now, time.Second, 10); send || after != 0 || !q.empty() {
		t.Fatal("nothing to send")
	}
	q.push(text(strings.Repeat("a", application.MaxProgressText)), now)
	q.push(text("b"), now)
	q.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "local_read"}, now)
	batch := q.take(now)
	if batch.Dropped != 1 || len(batch.Items) != 2 || batch.Items[0].Text != "b" || !q.empty() {
		t.Fatalf("%+v", batch)
	}
	q.push(text("c"), now)
	if send, after := q.due(now.Add(400*time.Millisecond), time.Second, 10); send || after != 600*time.Millisecond {
		t.Fatalf("%v %s", send, after)
	}
	q.close()
	q.push(text("d"), now)
	if !q.empty() {
		t.Fatal("a closed queue takes nothing")
	}
	many := newProgressQueue(application.MaxProgressItems + 5)
	for range application.MaxProgressItems + 3 {
		many.push(application.ProgressItem{Kind: application.ProgressToolEnded, CallID: "c"}, now)
	}
	many.push(text("left"), now)
	if first := many.take(now); len(first.Items) != application.MaxProgressItems {
		t.Fatalf("%d", len(first.Items))
	}
	if second := many.take(now); len(second.Items) != 4 || second.Items[3].Text != "left" {
		t.Fatalf("%+v", second)
	}
}

// A control plane older than the route, or one that keeps no more,
// stops the sending; a failed batch is logged once and the run goes on.
func TestTheSenderStopsWhenTheControlPlaneTakesNoMore(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(int) (application.ProgressAnswer, error)
		logs   string
	}{
		{"older", func(int) (application.ProgressAnswer, error) {
			return application.ProgressAnswer{}, domain.NotFoundError{Kind: "route", ID: "progress"}
		}, "takes no progress"},
		{"full", func(int) (application.ProgressAnswer, error) {
			return application.ProgressAnswer{Recorded: true, Full: true}, nil
		}, "keeps no more"},
	} {
		transport := &progressTransport{answer: c.answer}
		logs := &syncBuffer{}
		s := sender(transport, logs, Options{ProgressEvery: 10 * time.Millisecond})
		s.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "local_read"})
		deadline := time.Now().Add(2 * time.Second)
		for len(transport.sent()) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
		s.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "local_grep"})
		s.finish()
		if len(transport.sent()) != 1 || !strings.Contains(logs.String(), c.logs) {
			t.Errorf("%s: %d batches, logs %s", c.name, len(transport.sent()), logs.String())
		}
	}

	failing := &progressTransport{answer: func(int) (application.ProgressAnswer, error) {
		return application.ProgressAnswer{}, errors.New("connection refused")
	}}
	logs := &syncBuffer{}
	s := sender(failing, logs, Options{ProgressQueue: 1})
	s.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "a"})
	s.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "b"})
	s.push(application.ProgressItem{Kind: application.ProgressToolStarted, Tool: "c"})
	s.finish()
	if strings.Count(logs.String(), "connection refused") != 1 || !strings.Contains(logs.String(), "not handed over") {
		t.Errorf("logs = %s", logs.String())
	}

	// A run whose context ends stops its sender.
	ctx, cancel := context.WithCancel(context.Background())
	quiet := Agent{Transport: &progressTransport{}, Options: Options{}}.startProgress(ctx, "web-1", "exec-2")
	cancel()
	<-quiet.done
}
