package activityaudit

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// syncBuffer is a log both the watcher and the test touch.
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

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func endEvent(task string, kind domain.EventType) domain.Event {
	return domain.Event{ID: domain.EventID("evt-end-" + task), Type: kind, TaskID: domain.TaskID(task)}
}

// The watcher audits a task when a team's timeline tells its end, makes
// good a gap with a reconciliation, follows a team that formed, lets go
// of one that is gone, and stops with its context.
func TestTheWatcherAuditsEachTaskWhenItEnds(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"), team("qa", "qa-jefe"))
	source.pollFails["qa"] = 1
	publisher := &fakePublisher{}
	auditor, store := auditorFor(t, source, publisher, "")
	logs := &syncBuffer{}
	watcher, err := NewWatcher(WatcherConfig{Source: source, Auditor: auditor, Retry: time.Millisecond, Log: log.New(logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx, time.Time{}) }()
	eventually(t, "following", func() bool { return strings.Contains(logs.String(), "following 2 teams") })

	source.add("task-504", "dev-go", "Failed", provider504, at(61))
	source.polls["dev"] <- []domain.Event{{ID: "evt-progress", Type: domain.EventTaskProgress, TaskID: "task-504"}, endEvent("task-504", domain.EventTaskFailed), endEvent("task-504", domain.EventTaskFailed)}
	eventually(t, "the 504 published", func() bool { return len(publisher.all()) == 1 })

	// A bot in no team ends a task the timelines never tell: a full
	// window is a gap, and the reconciliation finds it.
	source.add("task-loner", "loner", "Failed", "disk full", at(62))
	listed := func() int { source.mu.Lock(); defer source.mu.Unlock(); return source.listed }
	before := listed()
	window := make([]domain.Event, application.MaxGroupEvents)
	for i := range window {
		window[i] = domain.Event{ID: domain.EventID("evt-w"), Type: domain.EventTaskProgress}
	}
	source.polls["dev"] <- window
	eventually(t, "the reconciliation", func() bool { return listed() > before && len(publisher.all()) == 2 })

	// A team that formed is followed after the next end; one that is
	// gone is let go.
	source.mu.Lock()
	source.groups = append(source.groups, team("marketing", "mkt-vigia"), team("gone"))
	source.polls["marketing"] = make(chan []domain.Event, 1)
	source.mu.Unlock()
	source.add("task-ok", "dev-go", "Completed", "", at(63))
	source.polls["dev"] <- []domain.Event{endEvent("task-ok", domain.EventTaskCompleted)}
	source.add("task-mkt", "mkt-vigia", "Failed", "watcher not available", at(64))
	eventually(t, "marketing followed", func() bool {
		select {
		case source.polls["marketing"] <- []domain.Event{endEvent("task-mkt", domain.EventTaskFailed)}:
		default:
		}
		return len(publisher.all()) == 3
	})
	if data := publisher.all()[2].Data; data["team"] != "marketing" {
		t.Errorf("the new team's bot = %+v", data)
	}

	// An audit that fails is logged and the watcher goes on.
	source.mu.Lock()
	source.failGet = context.DeadlineExceeded
	source.mu.Unlock()
	source.polls["qa"] <- []domain.Event{endEvent("task-x", domain.EventTaskCompleted)}
	eventually(t, "the failure logged", func() bool { return strings.Contains(logs.String(), "task task-x:") })
	if !strings.Contains(logs.String(), "team qa: the control plane went away") {
		t.Errorf("logs = %s", logs.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(store.Signatures()) != 3 {
		t.Errorf("signatures = %+v", store.Signatures())
	}
}

// Once stops after the start; a watcher without a source or an auditor
// is refused; a control plane that does not list its teams fails Run.
func TestTheWatcherOnceAndRefusals(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"))
	source.add("task-1", "dev-go", "Failed", provider504, at(1))
	publisher := &fakePublisher{}
	auditor, _ := auditorFor(t, source, publisher, "")
	watcher, err := NewWatcher(WatcherConfig{Source: source, Auditor: auditor, Once: true})
	if err != nil {
		t.Fatal(err)
	}
	if watcher.config.Wait != DefaultWait || watcher.config.Retry != DefaultRetry || watcher.config.Log == nil {
		t.Errorf("defaults = %+v", watcher.config)
	}
	if err := watcher.Run(context.Background(), at(0)); err != nil || len(publisher.all()) != 1 {
		t.Fatalf("once: %v %+v", err, publisher.all())
	}
	for _, config := range []WatcherConfig{{Source: source}, {Auditor: auditor}} {
		if _, err := NewWatcher(config); err == nil {
			t.Errorf("accepted %+v", config)
		}
	}
	broken, _ := NewWatcher(WatcherConfig{Source: failingGroups{source}, Auditor: auditor})
	if err := broken.Run(context.Background(), time.Time{}); err == nil || !strings.Contains(err.Error(), "list the teams") {
		t.Errorf("err = %v", err)
	}
}

type failingGroups struct{ *fakeSource }

func (failingGroups) ListGroups(context.Context) ([]application.Group, error) {
	return nil, context.Canceled
}

func TestReport(t *testing.T) {
	var out bytes.Buffer
	signatures := []SeenSignature{
		{ID: "fallo-1", Kind: KindToolCall, Tool: "local_exec", Program: "arreglo", Error: "no plan", Count: 3, Bots: []string{"a", "b", "c", "d"}, FirstAt: stamp(at(1)), PublishedAt: stamp(at(2))},
		{ID: "fallo-2", Kind: KindToolCall, Tool: "local_exec", Partial: true, Error: cutMarker, Count: 1, Bots: []string{"a"}, FirstAt: stamp(at(3))},
		{ID: "fallo-3", Kind: KindToolCall, Tool: toolCallTool, McpTool: "kmp/kmp_ask", Error: "x", Count: 1, FirstAt: stamp(at(3))},
		{ID: "fallo-4", Kind: KindTaskFailed, Error: "stuck", Count: 1, FirstAt: stamp(at(4))},
	}
	if err := Report(&out, signatures); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "local_exec arreglo", "a,b,c,+1", "2026-11-12T07:02", "local_exec (partial)", "axlr_call_tool kmp/kmp_ask", "task failed", " no "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out.String())
		}
	}
}
