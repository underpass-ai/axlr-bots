package activityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// testEvent is the event the tests publish, and testIgnorePrograms the
// programs they ignore: the operator's choices, no default of the adapter.
const testEvent = "activity.failure"

var testIgnorePrograms = []string{"sandbox-probe", "sandbox-term", "sandbox-gui", "sandbox-script"}

// The shapes the fleet writes today (12 Nov 2026): the node cuts a
// call's summary at 120 characters and the model writes its why first;
// AXLR cuts a failed call's error at 240 and writes a local or bridged
// call's answer as its response envelope.
const (
	cutSummary   = `{"why":"Remove abandoned sandboxes before exploration.","program":"/home/user-a/Documents/ai/example-app/teams/watchers…`
	cutEnvelope  = `{"protocol_version":1,"request_id":"call_fdb2fc6510c94379bfc2da2b7003bda2","tool":"exec","status":"completed","started_at":"2026-11-12T10:55:07.723408249Z","finished_at":"2026-11-12T10:55:07.735076746Z","duration_ms":11,"output":{"exit_cod…`
	bridgedCut   = `{"protocol_version":1,"request_id":"call_a738b9d7d0364b268438cb13e8e3a0c1","tool":"plugins.call","status":"completed","started_at":"2026-11-12T10:55:42.503944238Z","finished_at":"2026-11-12T10:55:42.51099906Z","duration_ms":7,"output":{"co…`
	rememberLine = `{"call_id":"call_3affcd07f7ad4c71870e2bc184eaa440","error":"send the continuation alone to commit a reviewed write"}`
	provider504  = "interrupted: model endpoint 127.0.0.1:8790 provider_failure (HTTP 504)"
)

// at is a moment of the test day.
func at(minute int) time.Time {
	return time.Date(2026, 11, 12, 7, minute, 0, 0, time.UTC)
}

// item is one progress item as the timeline's JSON decodes it.
type item map[string]any

func started(call, tool, summary string) item {
	return item{"kind": "tool.started", "tool": tool, "callId": call, "summary": summary}
}

func bridged(call, mcpTool string) item {
	return item{"kind": "tool.started", "tool": toolCallTool, "callId": call, "mcpTool": mcpTool, "summary": `{"about":"project:watchers"}`}
}

func ended(call, tool, status, errorText string, minute int) item {
	i := item{"kind": "tool.ended", "tool": tool, "callId": call, "status": status, "at": at(minute).Format(time.RFC3339Nano), "durationMs": 12}
	if errorText != "" {
		i["error"] = errorText
	}
	return i
}

// timelineOf is a task's timeline with one progress event per item.
func timelineOf(task string, items ...item) []domain.Event {
	events := []domain.Event{{ID: "evt-submitted-" + domain.EventID(task), Type: domain.EventTaskSubmitted, TaskID: domain.TaskID(task)}}
	for i, it := range items {
		raw, _ := json.Marshal(map[string]any{"items": []any{it}})
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		events = append(events, domain.Event{ID: domain.EventID(fmt.Sprintf("evt-%s-%d", task, i)), Type: domain.EventTaskProgress, TaskID: domain.TaskID(task), Payload: payload})
	}
	return events
}

// localExec is a local_exec's summary that shows the program and its
// arguments whole.
func localExec(program string, args ...string) string {
	raw, _ := json.Marshal(map[string]any{"program": program, "args": args})
	return string(raw)
}

// execFailed is a local_exec's whole envelope: the exit code and stderr.
func execFailed(call string, exit int, stderr string) string {
	raw, _ := json.Marshal(map[string]any{"protocol_version": 1, "request_id": call, "tool": "exec", "status": "completed", "output": map[string]any{"exit_code": exit, "stdout": "", "stderr": stderr}})
	return string(raw)
}

// fakeTask is one task the fake source answers for.
type fakeTask struct {
	snapshot   domain.TaskSnapshot
	executions []domain.ExecutionSnapshot
	timeline   []domain.Event
}

// fakeSource is the operator API in memory: teams, tasks, and the
// batches each team's long poll answers in turn.
type fakeSource struct {
	mu      sync.Mutex
	groups  []application.Group
	tasks   map[string]*fakeTask
	order   []string
	polls   map[string]chan []domain.Event
	listed  int
	failGet error
	// pollFails is how many long polls of a team fail before one answers.
	pollFails map[string]int
}

func newFakeSource(groups ...application.Group) *fakeSource {
	s := &fakeSource{groups: groups, tasks: map[string]*fakeTask{}, polls: map[string]chan []domain.Event{}, pollFails: map[string]int{}}
	for _, group := range groups {
		s.polls[group.Name] = make(chan []domain.Event, 8)
	}
	return s
}

func team(name string, bots ...string) application.Group {
	group := application.Group{TenantID: "default", Name: name}
	for _, bot := range bots {
		group.Members = append(group.Members, application.GroupMember{BotID: bot})
	}
	return group
}

// add records a task that ended with status, its calls in its timeline.
func (s *fakeSource) add(id, bot, status, reason string, created time.Time, items ...item) *fakeTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := &fakeTask{
		snapshot: domain.TaskSnapshot{ID: id, BotID: bot, Status: status, FailureReason: reason, CreatedAt: created, UpdatedAt: created.Add(time.Minute)},
		timeline: timelineOf(id, items...),
	}
	ended := created.Add(time.Minute)
	execution := domain.ExecutionSnapshot{ID: "exec-" + id, TaskID: id, BotID: bot, EndedAt: &ended}
	for _, it := range items {
		if it["kind"] == "tool.ended" {
			status := "completed"
			if it["status"] == "failed" {
				status = "failed"
			}
			execution.Result.ToolCalls = append(execution.Result.ToolCalls, domain.ToolCall{Name: it["tool"].(string), CallID: it["callId"].(string), Status: status})
		}
	}
	task.executions = []domain.ExecutionSnapshot{execution}
	s.tasks[id] = task
	s.order = append(s.order, id)
	return task
}

func (s *fakeSource) ListGroups(context.Context) ([]application.Group, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]application.Group(nil), s.groups...), nil
}

func (s *fakeSource) WaitGroupEvents(ctx context.Context, query application.GroupEventsQuery, wait time.Duration) ([]domain.Event, error) {
	s.mu.Lock()
	poll, ok := s.polls[query.Name]
	fails := s.pollFails[query.Name]
	if fails > 0 && wait > 0 {
		s.pollFails[query.Name] = fails - 1
	}
	s.mu.Unlock()
	if fails > 0 && wait > 0 {
		return nil, errors.New("the control plane went away")
	}
	if !ok {
		return nil, domain.NotFoundError{Kind: "group", ID: query.Name}
	}
	if wait == 0 {
		return []domain.Event{{ID: "evt-newest", Type: domain.EventTaskSubmitted}}, nil
	}
	select {
	case batch := <-poll:
		return batch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *fakeSource) ListTasks(_ context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listed++
	var tasks []domain.TaskSnapshot
	for i := len(s.order) - 1; i >= 0; i-- {
		task := s.tasks[s.order[i]].snapshot
		if !task.CreatedAt.Before(query.Since) {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (s *fakeSource) GetTask(_ context.Context, id string) (domain.TaskSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failGet != nil {
		return domain.TaskSnapshot{}, s.failGet
	}
	task, ok := s.tasks[id]
	if !ok {
		return domain.TaskSnapshot{}, domain.NotFoundError{Kind: "task", ID: id}
	}
	return task.snapshot, nil
}

func (s *fakeSource) ListExecutions(_ context.Context, id string) ([]domain.ExecutionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[id].executions, nil
}

func (s *fakeSource) ListTaskEvents(_ context.Context, query application.TaskEventsQuery) ([]domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[query.ID.String()].timeline, nil
}

// fakePublisher records what is published and answers as the control
// plane does: the event a key published before, created false.
type fakePublisher struct {
	mu        sync.Mutex
	published []application.PublishGroupEventCommand
	keys      map[string]string
	fail      error
}

func (p *fakePublisher) PublishGroupEvent(_ context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return application.PublishAnswer{}, p.fail
	}
	if p.keys == nil {
		p.keys = map[string]string{}
	}
	if id, ok := p.keys[command.IdempotencyKey]; ok {
		return application.PublishAnswer{EventID: id, Name: command.Name}, nil
	}
	id := fmt.Sprintf("evt-published-%d", len(p.published)+1)
	p.keys[command.IdempotencyKey] = id
	p.published = append(p.published, command)
	return application.PublishAnswer{EventID: id, Name: command.Name, Created: true}, nil
}

func (p *fakePublisher) all() []application.PublishGroupEventCommand {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]application.PublishGroupEventCommand(nil), p.published...)
}

// auditorFor is an auditor over the fake source and publisher, its
// store in the directory.
func auditorFor(t *testing.T, source *fakeSource, publisher *fakePublisher, dir string) (*Auditor, *SeenStore) {
	t.Helper()
	path := ""
	if dir != "" {
		path = dir + "/" + SeenFileName
	}
	store, err := OpenSeenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	config := AuditorConfig{Source: source, Store: store, Team: "watchers", Event: testEvent, IgnorePrograms: testIgnorePrograms, IgnoreTeamPartial: true, Now: func() time.Time { return at(59) }}
	if publisher != nil {
		config.Publisher = publisher
	}
	auditor, err := NewAuditor(config)
	if err != nil {
		t.Fatal(err)
	}
	groups, _ := source.ListGroups(context.Background())
	auditor.SetTeams(groups)
	return auditor, store
}
