package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// manualSignal is a ChangeSignal the test fires.
type manualSignal struct {
	mu sync.Mutex
	ch chan struct{}
}

func newManualSignal() *manualSignal { return &manualSignal{ch: make(chan struct{})} }

func (m *manualSignal) Next() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ch
}

func (m *manualSignal) fire() {
	m.mu.Lock()
	defer m.mu.Unlock()
	close(m.ch)
	m.ch = make(chan struct{})
}

// runningWorld is a root task still running, with one finished child.
func runningWorld(t *testing.T) *activityWorld {
	w := newActivityWorld(t)
	at := activityT0
	w.task(t, "root", domain.TaskRunning, []string{"kid"}, []string{"e-root"}, at, at.Add(time.Minute))
	w.task(t, "kid", domain.TaskCompleted, nil, nil, at.Add(time.Minute), at.Add(2*time.Minute))
	e, _ := domain.RestoreExecution(domain.ExecutionSnapshot{ID: "e-root", TaskID: "root", BotID: "bot-a", StartedAt: at, Outcome: "Running"})
	_ = w.execs.Save(context.Background(), e)
	w.note("root", domain.EventTaskSubmitted, at)
	return w
}

func TestWatchSendsASnapshotThenSmallUpdatesAndEndsWhenTheTreeFinishes(t *testing.T) {
	w := runningWorld(t)
	signal := newManualSignal()
	uc := NewWatchTaskActivity(w.deps, signal, time.Hour)
	var got []ActivityUpdate
	step := 0
	err := uc.Execute(context.Background(), "root", ActivityOptions{}, "", func(u ActivityUpdate) error {
		got = append(got, u)
		// The sender runs in the watch's goroutine, so the world can change
		// here without racing it.
		switch step {
		case 0:
			at := activityT0.Add(5 * time.Minute)
			w.note("root", domain.EventTaskStarted, at)
			w.task(t, "grand", domain.TaskRunning, nil, nil, at, at)
			w.task(t, "root", domain.TaskRunning, []string{"kid", "grand"}, []string{"e-root"}, activityT0, at)
			signal.fire()
		}
		step++
		if u.Child != nil {
			// Finish everything.
			end := activityT0.Add(9 * time.Minute)
			w.task(t, "grand", domain.TaskCompleted, nil, nil, activityT0.Add(5*time.Minute), end)
			w.task(t, "root", domain.TaskCompleted, []string{"kid", "grand"}, []string{"e-root"}, activityT0, end)
			w.execution(t, "e-root", "root", domain.Usage{}, nil, activityT0, end)
			signal.fire()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Snapshot == nil || got[0].Cursor == "" || got[0].Snapshot.Task.ID() != "root" {
		t.Fatalf("first: %+v", got[0])
	}
	var event, child, task, execution int
	for _, u := range got[1:] {
		switch {
		case u.Snapshot != nil:
			t.Error("a second snapshot")
		case u.Event != nil:
			event++
		case u.Child != nil:
			child++
			if u.Child.ParentTaskID != "root" || u.Child.Child.Task.ID() != "grand" {
				t.Errorf("child: %+v", u.Child)
			}
		case u.Task != nil:
			task++
		case u.Execution != nil:
			execution++
		}
		if _, ok := parseActivityCursor(u.Cursor); !ok {
			t.Errorf("bad cursor %q", u.Cursor)
		}
	}
	if event != 1 || child != 1 || task < 1 || execution != 1 {
		t.Errorf("updates: event %d child %d task %d execution %d (%d)", event, child, task, execution, len(got))
	}
	for i := 2; i < len(got); i++ {
		a, _ := parseActivityCursor(got[i-1].Cursor)
		b, _ := parseActivityCursor(got[i].Cursor)
		if b.Before(a) {
			t.Errorf("cursor went back at %d", i)
		}
	}
}

func TestWatchResumesFromACursorWithUpdatesNotASnapshot(t *testing.T) {
	w := runningWorld(t)
	signal := newManualSignal()
	uc := NewWatchTaskActivity(w.deps, signal, time.Hour)
	var cursor string
	_ = uc.Execute(context.Background(), "root", ActivityOptions{}, "", func(u ActivityUpdate) error {
		cursor = u.Cursor
		return errors.New("client lost")
	})
	if cursor == "" {
		t.Fatal("no cursor")
	}
	w.note("root", domain.EventTaskStarted, activityT0.Add(30*time.Minute))
	var got []ActivityUpdate
	_ = uc.Execute(context.Background(), "root", ActivityOptions{}, cursor, func(u ActivityUpdate) error {
		got = append(got, u)
		return errors.New("stop")
	})
	if len(got) != 1 || got[0].Event == nil || got[0].Snapshot != nil {
		t.Fatalf("resume: %+v", got)
	}
	// Nothing new: one heartbeat carries the cursor.
	got = nil
	last := got
	_ = uc.Execute(context.Background(), "root", ActivityOptions{}, activityCursor(activityT0.Add(time.Hour)), func(u ActivityUpdate) error {
		last = append(last, u)
		return errors.New("stop")
	})
	if len(last) != 1 || !last[0].Heartbeat || last[0].Cursor == "" {
		t.Errorf("nothing new: %+v", last)
	}
	// A cursor that is not ours starts over.
	var first ActivityUpdate
	_ = uc.Execute(context.Background(), "root", ActivityOptions{}, "garbage", func(u ActivityUpdate) error {
		first = u
		return errors.New("stop")
	})
	if first.Snapshot == nil {
		t.Errorf("garbage cursor: %+v", first)
	}
}

func TestWatchHeartbeatsWhenIdleAndStopsOnCancel(t *testing.T) {
	w := runningWorld(t)
	uc := NewWatchTaskActivity(w.deps, newManualSignal(), 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	var beats int
	err := uc.Execute(ctx, "root", ActivityOptions{}, "", func(u ActivityUpdate) error {
		if u.Heartbeat {
			beats++
			if beats == 2 {
				cancel()
			}
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || beats != 2 {
		t.Errorf("err %v beats %d", err, beats)
	}
}

func TestWatchCancelledWhileWaitingAndErrors(t *testing.T) {
	w := runningWorld(t)
	uc := NewWatchTaskActivity(w.deps, newManualSignal(), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	first := make(chan struct{})
	go func() {
		done <- uc.Execute(ctx, "root", ActivityOptions{}, "", func(ActivityUpdate) error {
			close(first)
			return nil
		})
	}()
	<-first
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not stop")
	}
	if err := uc.Execute(context.Background(), "nobody", ActivityOptions{}, "", func(ActivityUpdate) error { return nil }); err == nil {
		t.Error("watched a task that does not exist")
	}
	// The send of a context already over is refused between passes.
	done2, cancel2 := context.WithCancel(context.Background())
	err := uc.Execute(done2, "root", ActivityOptions{}, "", func(ActivityUpdate) error { cancel2(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancel in send: %v", err)
	}
	// A heartbeat that cannot be sent ends the watch.
	hb := NewWatchTaskActivity(w.deps, newManualSignal(), time.Millisecond)
	n := 0
	err = hb.Execute(context.Background(), "root", ActivityOptions{}, "", func(ActivityUpdate) error {
		n++
		if n == 2 {
			return errors.New("gone")
		}
		return nil
	})
	if err == nil || err.Error() != "gone" {
		t.Errorf("heartbeat send: %v", err)
	}
	if NewWatchTaskActivity(w.deps, newManualSignal(), 0).heartbeat != DefaultActivityHeartbeat {
		t.Error("default heartbeat")
	}
}

func TestWatchTellsACutAndItsCompletion(t *testing.T) {
	w, _ := cutWorld(t)
	// Make the execution still running with the cut recorded, then end it.
	e, _ := domain.RestoreExecution(domain.ExecutionSnapshot{ID: "e1", TaskID: "t1", BotID: "bot-a", StartedAt: activityT0, Outcome: "Running"})
	w.execs.executions["e1"] = e
	w.task(t, "t1", domain.TaskRunning, nil, []string{"e1"}, activityT0, activityT0.Add(5*time.Minute))
	signal := newManualSignal()
	uc := NewWatchTaskActivity(w.deps, signal, time.Hour)
	var completions []*ProgressCompletion
	n := 0
	err := uc.Execute(context.Background(), "t1", ActivityOptions{}, "", func(u ActivityUpdate) error {
		if u.Completed != nil {
			completions = append(completions, u.Completed)
		}
		n++
		if n == 1 {
			end := activityT0.Add(time.Hour)
			w.execs.executions["e1"] = mustEnded(t, end)
			w.task(t, "t1", domain.TaskCompleted, nil, []string{"e1"}, activityT0, end)
			signal.fire()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(completions) != 1 || len(completions[0].Items) != 4 || completions[0].ExecutionID != "e1" || completions[0].Cut.Completion != CompletionTranscript {
		t.Errorf("completions: %+v", completions)
	}
}

func mustEnded(t *testing.T, end time.Time) domain.Execution {
	e, err := domain.RestoreExecution(domain.ExecutionSnapshot{
		ID: "e1", TaskID: "t1", BotID: "bot-a", StartedAt: activityT0, EndedAt: &end, Outcome: "Completed",
		Result: domain.Result{Answer: "ok", Sessions: []string{"s1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestActivityCursorRoundTrip(t *testing.T) {
	at := time.Unix(1700000000, 123).UTC()
	got, ok := parseActivityCursor(activityCursor(at))
	if !ok || !got.Equal(at) {
		t.Errorf("%v %v", got, ok)
	}
	for _, bad := range []string{"", "a1.", "a1.x", "a1.-4", "b1.5"} {
		if _, ok := parseActivityCursor(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
