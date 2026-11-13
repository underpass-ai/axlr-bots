package grpcapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/filestore"
	"github.com/underpass-ai/axlr-bots/adapters/jsonl"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

var t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

// signal is the ChangeSignal of the tests.
type signal struct {
	mu sync.Mutex
	ch chan struct{}
}

func (s *signal) Next() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ch
}

func (s *signal) fire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.ch)
	s.ch = make(chan struct{})
}

// plane is a control plane over a temporary state directory and the
// activity service on a bufconn.
type plane struct {
	deps   application.Dependencies
	events *jsonl.EventLog
	signal *signal
	client operatorv1.ActivityServiceClient
	n      int
}

func newPlane(t *testing.T) *plane {
	t.Helper()
	dir := t.TempDir()
	events := jsonl.NewEventLog(dir)
	p := &plane{events: events, signal: &signal{ch: make(chan struct{})}}
	p.deps = application.Dependencies{
		Tasks: filestore.NewTaskRepository(dir), Executions: filestore.NewExecutionRepository(dir),
		EventReader: events, Events: events, Transcripts: filestore.NewTranscriptStore(dir), SessionItems: axlr.SessionItemReader{},
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	NewActivityService(p.deps, p.signal, 20*time.Millisecond).Register(server)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(); server.Stop() })
	p.client = operatorv1.NewActivityServiceClient(conn)
	return p
}

func (p *plane) task(t *testing.T, id string, status domain.TaskStatus, children, execs []string, at time.Time) {
	t.Helper()
	task, err := domain.RestoreTask(domain.TaskSnapshot{
		ID: id, BotID: "bot-a", Objective: "do " + id, Status: string(status), IdempotencyKey: "k-" + id,
		ExecutionIDs: execs, Children: children, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.deps.Tasks.Save(context.Background(), task); err != nil {
		t.Fatal(err)
	}
}

func (p *plane) execution(t *testing.T, id, task string, usage domain.Usage, sessions []string, at time.Time) {
	t.Helper()
	end := at.Add(time.Minute)
	e, err := domain.RestoreExecution(domain.ExecutionSnapshot{
		ID: id, TaskID: task, BotID: "bot-a", StartedAt: at, EndedAt: &end, Outcome: "Completed",
		Result: domain.Result{Answer: "ok", Sessions: sessions}, Usage: usage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.deps.Executions.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

func (p *plane) event(t *testing.T, task, exec string, kind domain.EventType, at time.Time, payload map[string]any) {
	t.Helper()
	p.n++
	e := domain.Event{
		ID: domain.EventID(fmt.Sprintf("ev-%d", p.n)), SchemaVersion: domain.EventSchemaV1, Type: kind, TenantID: "tenant",
		BotID: "bot-a", TaskID: domain.TaskID(task), ExecutionID: domain.ExecutionID(exec), CreatedAt: at, Payload: payload,
	}
	if err := p.events.Publish(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

// session is an AXLR session with five calls.
func session() []byte {
	var messages []map[string]any
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("c%d", i)
		messages = append(messages,
			map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{{"id": id, "name": "local_read", "arguments": map[string]any{"path": "x"}}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": fmt.Sprintf(`{"request_id":%q,"status":"completed","finished_at":"2026-11-14T10:%02d:00Z","duration_ms":5}`, id, i)},
		)
	}
	raw, _ := json.Marshal(map[string]any{"messages": messages})
	return raw
}

// world builds root -> kid -> grand and a cut execution on kid completed
// from its transcript.
func (p *plane) world(t *testing.T) {
	t.Helper()
	p.task(t, "root", domain.TaskCompleted, []string{"kid"}, []string{"e-root"}, t0)
	p.task(t, "kid", domain.TaskCompleted, []string{"grand"}, []string{"e-kid"}, t0.Add(time.Minute))
	p.task(t, "grand", domain.TaskCompleted, nil, nil, t0.Add(2*time.Minute))
	p.execution(t, "e-root", "root", domain.Usage{Requests: 1, CostUSD: 0.5}, nil, t0)
	p.execution(t, "e-kid", "kid", domain.Usage{CostUnknown: true, ToolCalls: 5}, []string{"s1"}, t0.Add(time.Minute))
	p.event(t, "root", "", domain.EventTaskSubmitted, t0, map[string]any{"objective": "x", "n": 1})
	for i := 0; i < application.MaxProgressEvents-1; i++ {
		p.event(t, "kid", "e-kid", domain.EventTaskProgress, t0.Add(time.Minute+time.Duration(i)*time.Millisecond), map[string]any{"items": []application.ProgressItem{{Kind: application.ProgressText, At: t0, Text: "w"}}})
	}
	kept := []application.ProgressItem{
		{Kind: application.ProgressToolStarted, At: t0.Add(time.Minute), Tool: "local_read", CallID: "c1"},
		{Kind: application.ProgressToolEnded, At: t0.Add(time.Minute), Tool: "local_read", CallID: "c1", Status: "ok"},
		{Kind: application.ProgressToolStarted, At: t0.Add(2 * time.Minute), Tool: "local_read", CallID: "c2"},
		{Kind: application.ProgressToolEnded, At: t0.Add(2 * time.Minute), Tool: "local_read", CallID: "c2", Status: "ok"},
	}
	p.event(t, "kid", "e-kid", domain.EventTaskProgress, t0.Add(3*time.Minute), map[string]any{"items": kept, "truncated": true, "dropped": 3})
	if _, err := p.deps.Transcripts.SaveTranscript(context.Background(), application.TranscriptRecord{Execution: "e-kid", Task: "kid", Bot: "bot-a", Session: "s1"}, session()); err != nil {
		t.Fatal(err)
	}
}

func TestGetTaskActivityOverGRPC(t *testing.T) {
	p := newPlane(t)
	p.world(t)
	resp, err := p.client.GetTaskActivity(context.Background(), &operatorv1.GetTaskActivityRequest{TaskId: "root"})
	if err != nil {
		t.Fatal(err)
	}
	a := resp.Activity
	if a.Task.Id != "root" || len(a.Events) != 1 || a.Events[0].Payload.Fields["objective"].GetStringValue() != "x" {
		t.Fatalf("root: %+v", a)
	}
	kid := a.Children[0]
	if kid.Task.Id != "kid" || len(kid.Children) != 1 || kid.Children[0].Task.Id != "grand" {
		t.Fatalf("tree: %+v", kid)
	}
	if !a.SubtreeUsage.CostUnknown || a.SubtreeUsage.CostUsd != 0.5 || a.SubtreeUsage.ToolCalls != 5 || a.SubtreeUsage.Requests != 1 {
		t.Errorf("subtree usage: %+v", a.SubtreeUsage)
	}
	if len(kid.Events) != application.MaxProgressEvents {
		t.Errorf("kid timeline %d", len(kid.Events))
	}
	e := kid.Executions[0]
	cut := e.ProgressCut
	if cut == nil || cut.Reason != operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_EVENT_LIMIT || cut.Dropped != 3 ||
		cut.Completion != operatorv1.CompletionSource_COMPLETION_SOURCE_TRANSCRIPT || len(cut.TranscriptSessions) != 1 || cut.EventsKept != application.MaxProgressEvents {
		t.Fatalf("cut: %+v", cut)
	}
	if len(e.CompletedItems) != 6 || e.CompletedItems[0].CallId != "c3" || e.CompletedItems[5].CallId != "c5" || e.CompletedItems[1].Duration.AsDuration() != 5*time.Millisecond {
		t.Errorf("completed items: %v", e.CompletedItems)
	}
	if e.Execution.Result.Sessions[0] != "s1" || a.Executions[0].ProgressCut != nil {
		t.Errorf("execution mapping")
	}

	alone, err := p.client.GetTaskActivity(context.Background(), &operatorv1.GetTaskActivityRequest{TaskId: "root", Options: &operatorv1.ActivityOptions{ExcludeChildren: true}})
	if err != nil || len(alone.Activity.Children) != 0 {
		t.Errorf("exclude children: %v", err)
	}
	skipped, _ := p.client.GetTaskActivity(context.Background(), &operatorv1.GetTaskActivityRequest{TaskId: "kid", Options: &operatorv1.ActivityOptions{SkipCompletedProgress: true, MaxDepth: 1}})
	if c := skipped.Activity.Executions[0]; len(c.CompletedItems) != 0 || c.ProgressCut.Completion != operatorv1.CompletionSource_COMPLETION_SOURCE_NONE {
		t.Errorf("skip: %+v", c)
	}
}

func TestGetTaskActivityErrors(t *testing.T) {
	p := newPlane(t)
	_, err := p.client.GetTaskActivity(context.Background(), &operatorv1.GetTaskActivityRequest{TaskId: "nobody"})
	st := status.Convert(err)
	if st.Code() != codes.NotFound || len(st.Details()) != 1 || st.Details()[0].(*operatorv1.ErrorDetail).Kind != "not_found" {
		t.Errorf("not found: %v", err)
	}
	for name, req := range map[string]*operatorv1.GetTaskActivityRequest{
		"empty id":  {},
		"depth":     {TaskId: "x", Options: &operatorv1.ActivityOptions{MaxDepth: -1}},
		"completed": {TaskId: "x", Options: &operatorv1.ActivityOptions{MaxCompletedItems: -1}},
	} {
		if _, err := p.client.GetTaskActivity(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := p.client.WatchTaskActivity(context.Background(), &operatorv1.WatchTaskActivityRequest{TaskId: ""}); err != nil {
		// The error of a stream comes with the first receive.
		t.Fatal(err)
	}
}

func TestActivityErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{nil, codes.OK},
		{domain.StateError{Subject: "x", Current: "a", Attempted: "b"}, codes.FailedPrecondition},
		{errors.New("boom"), codes.Internal},
		{context.Canceled, codes.Canceled},
		{context.DeadlineExceeded, codes.DeadlineExceeded},
		{status.Error(codes.Aborted, "kept"), codes.Aborted},
	} {
		if got := status.Code(activityError(tc.err)); got != tc.code {
			t.Errorf("%v: %v", tc.err, got)
		}
	}
}

func TestWatchTaskActivityStreamsAndStopsOnCancel(t *testing.T) {
	p := newPlane(t)
	p.task(t, "w1", domain.TaskRunning, nil, nil, t0)
	p.event(t, "w1", "", domain.EventTaskSubmitted, t0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := p.client.WatchTaskActivity(ctx, &operatorv1.WatchTaskActivityRequest{TaskId: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil || first.GetSnapshot() == nil || first.Cursor == "" || first.GetSnapshot().Task.Id != "w1" {
		t.Fatalf("snapshot: %v %v", first, err)
	}
	// A new event comes as a small update.
	p.event(t, "w1", "", domain.EventTaskStarted, t0.Add(time.Hour), nil)
	p.signal.fire()
	var got *operatorv1.WatchTaskActivityResponse
	for got == nil || got.GetEventAppended() == nil {
		if got, err = stream.Recv(); err != nil {
			t.Fatal(err)
		}
		if got.GetSnapshot() != nil {
			t.Fatal("second snapshot")
		}
	}
	if got.GetEventAppended().TaskId != "w1" || got.GetEventAppended().Event.Type != string(domain.EventTaskStarted) {
		t.Errorf("event update: %v", got)
	}
	// Idle: a heartbeat, and the stream resumes from its cursor.
	var beat *operatorv1.WatchTaskActivityResponse
	for beat == nil || beat.GetHeartbeat() == nil {
		if beat, err = stream.Recv(); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	for {
		if _, err = stream.Recv(); err != nil {
			break
		}
	}
	if status.Code(err) != codes.Canceled {
		t.Errorf("after cancel: %v", err)
	}

	p.event(t, "w1", "", domain.EventTaskCompleted, t0.Add(2*time.Hour), nil)
	resumed, err := p.client.WatchTaskActivity(context.Background(), &operatorv1.WatchTaskActivityRequest{TaskId: "w1", ResumeCursor: first.Cursor})
	if err != nil {
		t.Fatal(err)
	}
	r, err := resumed.Recv()
	if err != nil || r.GetSnapshot() != nil || r.GetEventAppended() == nil {
		t.Errorf("resume: %v %v", r, err)
	}
}

func TestWatchTaskActivityEndsWithOKWhenTheTreeFinished(t *testing.T) {
	p := newPlane(t)
	p.world(t)
	stream, err := p.client.WatchTaskActivity(context.Background(), &operatorv1.WatchTaskActivityRequest{TaskId: "root"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := stream.Recv()
	if err != nil || msg.GetSnapshot() == nil {
		t.Fatalf("snapshot: %v %v", msg, err)
	}
	if _, err := stream.Recv(); err.Error() != "EOF" {
		t.Errorf("stream end: %v", err)
	}
	bad, _ := p.client.WatchTaskActivity(context.Background(), &operatorv1.WatchTaskActivityRequest{TaskId: "nobody"})
	if _, err := bad.Recv(); status.Code(err) != codes.NotFound {
		t.Errorf("watch of nothing: %v", err)
	}
	bad, _ = p.client.WatchTaskActivity(context.Background(), &operatorv1.WatchTaskActivityRequest{TaskId: ""})
	if _, err := bad.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Errorf("watch of no id: %v", err)
	}
}

func TestEveryUpdateKindIsMapped(t *testing.T) {
	task, _ := domain.RestoreTask(domain.TaskSnapshot{ID: "t", BotID: "b", Objective: "o", Status: "Pending", IdempotencyKey: "k", CreatedAt: t0, UpdatedAt: t0})
	exec, _ := domain.RestoreExecution(domain.ExecutionSnapshot{ID: "e", TaskID: "t", BotID: "b", StartedAt: t0, Outcome: "Running"})
	tree := application.TaskActivity{Task: task}
	cut := application.ProgressCut{Reason: application.CutByteLimit, Completion: application.CompletionNone}
	updates := []application.ActivityUpdate{
		{Cursor: "c", Snapshot: &tree},
		{Cursor: "c", Task: &task},
		{Cursor: "c", Execution: &exec},
		{Cursor: "c", Event: &application.ActivityEvent{TaskID: "t", Event: domain.Event{ID: "x", Payload: map[string]any{"items": []application.ProgressItem{{Kind: application.ProgressText}}}}}},
		{Cursor: "c", Child: &application.ChildAttached{ParentTaskID: "t", Child: tree}},
		{Cursor: "c", Completed: &application.ProgressCompletion{TaskID: "t", ExecutionID: "e", Cut: cut}},
		{Cursor: "c", Heartbeat: true},
	}
	for i, u := range updates {
		got := activityResponse(u)
		if got.Cursor != "c" || got.Update == nil {
			t.Errorf("update %d: %v", i, got)
		}
	}
	if activityCutReason("") != operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_UNSPECIFIED || activityCompletion("") != operatorv1.CompletionSource_COMPLETION_SOURCE_UNSPECIFIED {
		t.Error("unknown enums")
	}
	if activityCutReason(application.CutBatchTruncated) != operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_BATCH_TRUNCATED || activityTime(time.Time{}) != nil {
		t.Error("cut reasons")
	}
	if jsonSafe(map[string]any{"c": make(chan int)}) != nil {
		t.Error("an unmarshalable payload is left out")
	}
}
