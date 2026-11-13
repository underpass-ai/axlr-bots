package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

var activityT0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// fixtureReader reads "items" of a test transcript: a JSON array of
// ProgressItem.
type fixtureReader struct{ err error }

func (f fixtureReader) ReadItems(content []byte) ([]ProgressItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	var items []ProgressItem
	err := json.Unmarshal(content, &items)
	return items, err
}

// activityWorld is a control plane in memory.
type activityWorld struct {
	deps    Dependencies
	tasks   *memoryTasks
	execs   *memoryExecutions
	log     *collector
	scripts *memTranscripts
	n       int
}

func newActivityWorld(t *testing.T) *activityWorld {
	t.Helper()
	w := &activityWorld{tasks: &memoryTasks{}, execs: &memoryExecutions{}, log: &collector{}, scripts: &memTranscripts{}}
	w.deps = Dependencies{Tasks: w.tasks, Executions: w.execs, EventReader: w.log, Transcripts: w.scripts, SessionItems: fixtureReader{}}
	return w
}

// task saves a task under the id with the children and executions.
func (w *activityWorld) task(t *testing.T, id string, status domain.TaskStatus, children []string, execs []string, created, updated time.Time) {
	t.Helper()
	snap := domain.TaskSnapshot{
		ID: id, BotID: "bot-a", Objective: "do " + id, Status: string(status), IdempotencyKey: "key-" + id,
		ExecutionIDs: execs, Children: children, CreatedAt: created, UpdatedAt: updated,
	}
	task, err := domain.RestoreTask(snap)
	if err != nil {
		t.Fatalf("task %s: %v", id, err)
	}
	_ = w.tasks.Save(context.Background(), task)
}

// execution saves an ended execution with the usage and sessions.
func (w *activityWorld) execution(t *testing.T, id, task string, usage domain.Usage, sessions []string, started, ended time.Time) {
	t.Helper()
	snap := domain.ExecutionSnapshot{
		ID: id, TaskID: task, BotID: "bot-a", StartedAt: started, EndedAt: &ended, Outcome: "Completed",
		Result: domain.Result{Answer: "done", Sessions: sessions}, Usage: usage,
	}
	if usage.CostUnknown {
		snap.Outcome = "Cancelled"
		snap.FailureReason = "cancelled"
		snap.Result = domain.Result{}
	}
	e, err := domain.RestoreExecution(snap)
	if err != nil {
		t.Fatalf("execution %s: %v", id, err)
	}
	_ = w.execs.Save(context.Background(), e)
}

// progress publishes a bot.task.progress event of the execution.
func (w *activityWorld) progress(task, exec string, at time.Time, truncated bool, items ...ProgressItem) {
	w.n++
	payload := map[string]any{"items": items}
	if truncated {
		payload["truncated"] = true
	}
	w.log.events = append(w.log.events, domain.Event{
		ID: domain.EventID(fmt.Sprintf("ev-%d", w.n)), Type: domain.EventTaskProgress, TaskID: domain.TaskID(task),
		ExecutionID: domain.ExecutionID(exec), CreatedAt: at, Payload: payload,
	})
}

func (w *activityWorld) note(task string, kind domain.EventType, at time.Time) {
	w.n++
	w.log.events = append(w.log.events, domain.Event{ID: domain.EventID(fmt.Sprintf("ev-%d", w.n)), Type: kind, TaskID: domain.TaskID(task), CreatedAt: at})
}

func (w *activityWorld) transcript(t *testing.T, exec, session string, items []ProgressItem) {
	t.Helper()
	raw, _ := json.Marshal(items)
	if _, err := w.scripts.SaveTranscript(context.Background(), TranscriptRecord{Execution: domain.ExecutionID(exec), Session: session}, raw); err != nil {
		t.Fatal(err)
	}
}

func call(n int, at time.Time) []ProgressItem {
	id := fmt.Sprintf("c%d", n)
	return []ProgressItem{
		{Kind: ProgressToolStarted, At: at, Tool: "t", CallID: id},
		{Kind: ProgressToolEnded, At: at.Add(time.Second), Tool: "t", CallID: id, Status: "ok"},
	}
}

// tree builds root -> (kid -> grandkid, other), each with an execution.
func (w *activityWorld) tree(t *testing.T) {
	t.Helper()
	at := activityT0
	w.task(t, "root", domain.TaskCompleted, []string{"kid", "other"}, []string{"e-root"}, at, at.Add(10*time.Minute))
	w.task(t, "kid", domain.TaskCompleted, []string{"grandkid"}, []string{"e-kid"}, at.Add(time.Minute), at.Add(9*time.Minute))
	w.task(t, "grandkid", domain.TaskCompleted, nil, []string{"e-grand"}, at.Add(2*time.Minute), at.Add(8*time.Minute))
	w.task(t, "other", domain.TaskCompleted, nil, []string{"e-other"}, at.Add(time.Minute), at.Add(7*time.Minute))
	w.execution(t, "e-root", "root", domain.Usage{Requests: 1, CostUSD: 0.5, ToolCalls: 2}, nil, at, at.Add(10*time.Minute))
	w.execution(t, "e-kid", "kid", domain.Usage{Requests: 2, CostUSD: 0.25}, nil, at.Add(time.Minute), at.Add(9*time.Minute))
	w.execution(t, "e-grand", "grandkid", domain.Usage{Requests: 3, CostUSD: 0.125}, nil, at.Add(2*time.Minute), at.Add(8*time.Minute))
	w.execution(t, "e-other", "other", domain.Usage{CostUnknown: true, ToolCalls: 4}, nil, at.Add(time.Minute), at.Add(7*time.Minute))
	w.note("root", domain.EventTaskSubmitted, at)
	w.note("grandkid", domain.EventTaskSubmitted, at.Add(2*time.Minute))
}

func TestGetTaskActivityBuildsTheTreeWithSubtreeUsage(t *testing.T) {
	w := newActivityWorld(t)
	w.tree(t)
	got, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "root", ActivityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Children) != 2 || got.Children[0].Task.ID() != "kid" || len(got.Children[0].Children) != 1 || got.Children[0].Children[0].Task.ID() != "grandkid" {
		t.Fatalf("tree: %+v", got.Children)
	}
	if len(got.Executions) != 1 || len(got.Events) != 1 || len(got.Children[0].Children[0].Events) != 1 {
		t.Errorf("executions %d events %d", len(got.Executions), len(got.Events))
	}
	u := got.SubtreeUsage
	if u.Requests != 6 || u.CostUSD != 0.875 || u.ToolCalls != 6 || !u.CostUnknown {
		t.Errorf("subtree usage %+v", u)
	}
	if kid := got.Children[0].SubtreeUsage; kid.Requests != 5 || kid.CostUSD != 0.375 || kid.CostUnknown {
		t.Errorf("kid usage %+v", kid)
	}
}

func TestGetTaskActivityHonoursDepthAndExcludeChildren(t *testing.T) {
	w := newActivityWorld(t)
	w.tree(t)
	uc := NewGetTaskActivity(w.deps)
	one, _ := uc.Execute(context.Background(), "root", ActivityOptions{MaxDepth: 1})
	if len(one.Children) != 2 || len(one.Children[0].Children) != 0 {
		t.Errorf("max depth 1: %+v", one.Children)
	}
	if one.Children[0].SubtreeUsage.Requests != 2 {
		t.Errorf("a level not followed is not summed: %+v", one.Children[0].SubtreeUsage)
	}
	alone, _ := uc.Execute(context.Background(), "root", ActivityOptions{ExcludeChildren: true})
	if len(alone.Children) != 0 || alone.SubtreeUsage.Requests != 1 {
		t.Errorf("exclude children: %+v", alone)
	}
	if _, err := uc.Execute(context.Background(), "nobody", ActivityOptions{}); err == nil {
		t.Error("a task that does not exist was answered")
	}
	if d := (ActivityOptions{MaxDepth: 99}).depth(); d != domain.MaxDelegationDepthLimit {
		t.Errorf("depth over the limit: %d", d)
	}
}

func TestGetTaskActivityStopsAtACycleAndSkipsVanishedChildren(t *testing.T) {
	w := newActivityWorld(t)
	at := activityT0
	w.task(t, "a", domain.TaskRunning, []string{"b", "gone"}, nil, at, at)
	w.task(t, "b", domain.TaskRunning, []string{"a", "b"}, []string{"missing-exec"}, at, at)
	got, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "a", ActivityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Children) != 1 || len(got.Children[0].Children) != 0 || len(got.Children[0].Executions) != 0 {
		t.Errorf("cycle: %+v", got)
	}
}

func TestGetTaskActivityPropagatesStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	w := newActivityWorld(t)
	w.tree(t)
	w.log.fail = boom
	if _, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "root", ActivityOptions{}); !errors.Is(err, boom) {
		t.Errorf("events: %v", err)
	}
	w.log.fail = nil
	w.deps.Executions = failingExecutions{boom}
	if _, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "root", ActivityOptions{}); !errors.Is(err, boom) {
		t.Errorf("executions: %v", err)
	}
	w.deps.Executions = w.execs
	w.deps.Tasks = failingChild{w.tasks, "kid", boom}
	if _, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "root", ActivityOptions{}); !errors.Is(err, boom) {
		t.Errorf("child: %v", err)
	}
}

type failingExecutions struct{ err error }

func (f failingExecutions) Save(context.Context, domain.Execution) error { return f.err }
func (f failingExecutions) Get(context.Context, domain.ExecutionID) (domain.Execution, error) {
	return domain.Execution{}, f.err
}
func (f failingExecutions) ListRunning(context.Context) ([]domain.Execution, error) {
	return nil, f.err
}
func (f failingExecutions) List(context.Context) ([]domain.Execution, error) { return nil, f.err }

type failingChild struct {
	*memoryTasks
	id  domain.TaskID
	err error
}

func (f failingChild) Get(ctx context.Context, id domain.TaskID) (domain.Task, error) {
	if id == f.id {
		return domain.Task{}, f.err
	}
	return f.memoryTasks.Get(ctx, id)
}

func TestTimelineIsReadWholeBeyondOnePage(t *testing.T) {
	w := newActivityWorld(t)
	at := activityT0
	w.task(t, "big", domain.TaskRunning, nil, nil, at, at)
	for i := 0; i < MaxEventLimit+250; i++ {
		w.note("big", domain.EventTaskStarted, at.Add(time.Duration(i)*time.Millisecond))
	}
	got, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "big", ActivityOptions{})
	if err != nil || len(got.Events) != MaxEventLimit+250 {
		t.Fatalf("%d events: %v", len(got.Events), err)
	}
	if got.Events[0].CreatedAt != at {
		t.Error("oldest first")
	}
}

// cutWorld is a task whose only execution hit the event limit after its
// third call, with a transcript that goes on for two more.
func cutWorld(t *testing.T) (*activityWorld, []ProgressItem) {
	w := newActivityWorld(t)
	at := activityT0
	w.task(t, "t1", domain.TaskCompleted, nil, []string{"e1"}, at, at.Add(time.Hour))
	w.execution(t, "e1", "t1", domain.Usage{}, []string{"s1"}, at, at.Add(time.Hour))
	var all []ProgressItem
	for i := 1; i <= 5; i++ {
		all = append(all, call(i, at.Add(time.Duration(i)*time.Minute))...)
	}
	// What the timeline kept: calls 1 to 3, over MaxProgressEvents events
	// (the rest of the events are padding words from before).
	for i := 0; i < MaxProgressEvents-1; i++ {
		w.progress("t1", "e1", at.Add(time.Duration(i)*time.Millisecond), false, ProgressItem{Kind: ProgressText, At: at, Text: "w"})
	}
	var kept []ProgressItem
	for i := 1; i <= 3; i++ {
		kept = append(kept, call(i, at.Add(time.Duration(i)*time.Minute))...)
	}
	w.progress("t1", "e1", at.Add(4*time.Minute), true, kept...)
	w.transcript(t, "e1", "s1", all)
	return w, all
}

func TestGetTaskActivityCompletesACutFromTheTranscript(t *testing.T) {
	w, _ := cutWorld(t)
	got, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "t1", ActivityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e := got.Executions[0]
	if e.Cut == nil || e.Cut.Reason != CutEventLimit || e.Cut.EventsKept != MaxProgressEvents || e.Cut.Completion != CompletionTranscript {
		t.Fatalf("cut: %+v", e.Cut)
	}
	if len(e.Cut.TranscriptSessions) != 1 || e.Cut.TranscriptSessions[0] != "s1" || e.Cut.CompletionBounded {
		t.Errorf("sessions: %+v", e.Cut)
	}
	if len(e.Completed) != 4 || e.Completed[0].CallID != "c4" || e.Completed[3].CallID != "c5" || e.Completed[3].Kind != ProgressToolEnded {
		t.Errorf("completed: %+v", e.Completed)
	}
	if !e.Cut.CutAt.Equal(activityT0.Add(3*time.Minute + time.Second)) {
		t.Errorf("cut at %v", e.Cut.CutAt)
	}
}

func TestCompletionOptionsAndLimits(t *testing.T) {
	w, _ := cutWorld(t)
	uc := NewGetTaskActivity(w.deps)
	skip, _ := uc.Execute(context.Background(), "t1", ActivityOptions{SkipCompletedProgress: true})
	if c := skip.Executions[0].Cut; c == nil || c.Completion != CompletionNone || len(skip.Executions[0].Completed) != 0 {
		t.Errorf("skip: %+v", skip.Executions[0])
	}
	bounded, _ := uc.Execute(context.Background(), "t1", ActivityOptions{MaxCompletedItems: 3})
	if e := bounded.Executions[0]; len(e.Completed) != 3 || !e.Cut.CompletionBounded {
		t.Errorf("bounded: %+v", e)
	}
	// No transcript kept: the cut stays marked.
	w.scripts.rows = nil
	none, _ := uc.Execute(context.Background(), "t1", ActivityOptions{})
	if e := none.Executions[0]; e.Cut == nil || e.Cut.Completion != CompletionNone || len(e.Completed) != 0 {
		t.Errorf("no transcript: %+v", e)
	}
	// A store that fails is an error; a transcript that cannot be read is not.
	w.scripts.fail = errors.New("disk")
	if _, err := uc.Execute(context.Background(), "t1", ActivityOptions{}); err == nil {
		t.Error("store failure swallowed")
	}
	w.scripts.fail = nil
	w, _ = cutWorld(t)
	w.deps.SessionItems = fixtureReader{err: errors.New("bad json")}
	bad, err := NewGetTaskActivity(w.deps).Execute(context.Background(), "t1", ActivityOptions{})
	if err != nil || bad.Executions[0].Cut.Completion != CompletionNone {
		t.Errorf("unreadable transcript: %v %+v", err, bad.Executions[0].Cut)
	}
	w.deps.SessionItems = nil
	if r, _ := NewGetTaskActivity(w.deps).Execute(context.Background(), "t1", ActivityOptions{}); r.Executions[0].Cut == nil {
		t.Error("cut lost without a reader")
	}
	if (ActivityOptions{}).completedLimit() != DefaultMaxCompletedItems {
		t.Error("default limit")
	}
}

func TestCutReasonsAndNoCut(t *testing.T) {
	at := activityT0
	whole := recordedProgress{events: 3, bytes: 100}
	if whole.cut() != nil {
		t.Error("a whole timeline has no cut")
	}
	if c := (recordedProgress{events: 3, bytes: MaxProgressBytes}).cut(); c == nil || c.Reason != CutByteLimit {
		t.Errorf("bytes: %+v", c)
	}
	c := (recordedProgress{events: 3, bytes: 10, truncated: true, dropped: 7, lastEvent: at}).cut()
	if c == nil || c.Reason != CutBatchTruncated || c.Dropped != 7 || !c.CutAt.Equal(at) {
		t.Errorf("truncated: %+v", c)
	}
	// dropped counts survive the JSON a log read back gives.
	ev := domain.Event{Type: domain.EventTaskProgress, ExecutionID: "x", Payload: map[string]any{"items": []any{}, "dropped": float64(2), "truncated": true}}
	if got := progressOf([]domain.Event{ev}, "x"); got.dropped != 2 || !got.truncated {
		t.Errorf("progressOf: %+v", got)
	}
	if payloadCount(int64(3)) != 3 || payloadCount(4) != 4 || payloadCount("x") != 0 {
		t.Error("payloadCount")
	}
}

func TestItemsAfterWithoutACallToAnchorOn(t *testing.T) {
	at := activityT0
	words := []ProgressItem{{Kind: ProgressText, At: at, Text: "a"}}
	transcript := []ProgressItem{
		{Kind: ProgressText, At: at, Text: "a"},
		{Kind: ProgressToolStarted, At: at.Add(time.Minute), Tool: "t", CallID: "x"},
	}
	got := itemsAfter(transcript, words, at)
	if len(got) != 1 || got[0].CallID != "x" {
		t.Errorf("by time: %+v", got)
	}
	if got := itemsAfter(transcript, nil, at); len(got) != 2 {
		t.Errorf("nothing kept: %+v", got)
	}
}

func TestSessionsOfListsEachSessionOnce(t *testing.T) {
	got := sessionsOf(domain.Result{Sessions: []string{"a", "b", "a", "../x"}, SessionID: "b"})
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("%v", got)
	}
}
