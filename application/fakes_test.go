package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

var t0 = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

type memoryBots struct {
	mu   sync.Mutex
	bots map[domain.BotID]domain.Bot
	fail error
}

func (m *memoryBots) Save(_ context.Context, bot domain.Bot) error {
	if m.fail != nil {
		return m.fail
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bots == nil {
		m.bots = map[domain.BotID]domain.Bot{}
	}
	m.bots[bot.ID()] = bot
	return nil
}

func (m *memoryBots) Get(_ context.Context, id domain.BotID) (domain.Bot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bot, ok := m.bots[id]
	if !ok {
		return domain.Bot{}, domain.NotFoundError{Kind: "bot", ID: id.String()}
	}
	return bot, nil
}

func (m *memoryBots) List(context.Context) ([]domain.Bot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.Bot, 0, len(m.bots))
	for _, b := range m.bots {
		out = append(out, b)
	}
	return out, nil
}

type memoryTasks struct {
	tasks map[domain.TaskID]domain.Task
	fail  error
	saves int
}

func (m *memoryTasks) Save(_ context.Context, task domain.Task) error {
	if m.fail != nil {
		return m.fail
	}
	if m.tasks == nil {
		m.tasks = map[domain.TaskID]domain.Task{}
	}
	m.tasks[task.ID()] = task
	m.saves++
	return nil
}

func (m *memoryTasks) Get(_ context.Context, id domain.TaskID) (domain.Task, error) {
	task, ok := m.tasks[id]
	if !ok {
		return domain.Task{}, domain.NotFoundError{Kind: "task", ID: id.String()}
	}
	return task, nil
}

func (m *memoryTasks) ListByStatus(_ context.Context, status domain.TaskStatus) ([]domain.Task, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	var out []domain.Task
	for _, t := range m.tasks {
		if t.Status() == status {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b domain.Task) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

func (m *memoryTasks) FindByIdempotencyKey(_ context.Context, bot domain.BotID, key domain.IdempotencyKey) (domain.Task, bool, error) {
	for _, t := range m.tasks {
		if t.BotID() == bot && t.IdempotencyKey() == key {
			return t, true, nil
		}
	}
	return domain.Task{}, false, nil
}

type memoryExecutions struct {
	executions map[domain.ExecutionID]domain.Execution
	fail       error
}

func (m *memoryExecutions) Save(_ context.Context, e domain.Execution) error {
	if m.fail != nil {
		return m.fail
	}
	if m.executions == nil {
		m.executions = map[domain.ExecutionID]domain.Execution{}
	}
	m.executions[e.ID()] = e
	return nil
}

func (m *memoryExecutions) Get(_ context.Context, id domain.ExecutionID) (domain.Execution, error) {
	e, ok := m.executions[id]
	if !ok {
		return domain.Execution{}, domain.NotFoundError{Kind: "execution", ID: id.String()}
	}
	return e, nil
}

type collector struct {
	events []domain.Event
	audits []domain.AuditEntry
	fail   error
}

func (c *collector) Publish(_ context.Context, e domain.Event) error {
	if c.fail != nil {
		return c.fail
	}
	c.events = append(c.events, e)
	return nil
}

func (c *collector) Record(_ context.Context, a domain.AuditEntry) error {
	if c.fail != nil {
		return c.fail
	}
	c.audits = append(c.audits, a)
	return nil
}

func (c *collector) eventTypes() []string {
	out := make([]string, len(c.events))
	for i, e := range c.events {
		out[i] = e.Type.String()
	}
	return out
}

// fakeRuntime is the fake RuntimeExecutor: it records requests and
// answers with a scripted report.
type fakeRuntime struct {
	requests []ExecutionRequest
	report   ExecutionReport
	err      error
}

func (f *fakeRuntime) Execute(_ context.Context, request ExecutionRequest) (ExecutionReport, error) {
	f.requests = append(f.requests, request)
	return f.report, f.err
}

// fakeTraces is the fake TraceLocator: the traces it knows about.
type fakeTraces struct {
	paths map[domain.ExecutionID]string
}

func (f *fakeTraces) TracePath(execution domain.ExecutionID) (string, bool) {
	path, ok := f.paths[execution]
	return path, ok
}

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

type sequentialIDs struct{ n int }

func (s *sequentialIDs) NewID(prefix string) string {
	s.n++
	return fmt.Sprintf("%s%d", prefix, s.n)
}

type harness struct {
	deps    Dependencies
	bots    *memoryBots
	tasks   *memoryTasks
	execs   *memoryExecutions
	log     *collector
	runtime *fakeRuntime
	traces  *fakeTraces
	clock   *fixedClock
}

func newHarness() *harness {
	h := &harness{
		bots:    &memoryBots{},
		tasks:   &memoryTasks{},
		execs:   &memoryExecutions{},
		log:     &collector{},
		runtime: &fakeRuntime{report: ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}, TracePath: "/traces/exec-1.jsonl"}},
		traces:  &fakeTraces{paths: map[domain.ExecutionID]string{}},
		clock:   &fixedClock{now: t0},
	}
	h.deps = Dependencies{
		Bots: h.bots, Tasks: h.tasks, Executions: h.execs, Events: h.log, Audit: h.log,
		Runtime: h.runtime, Traces: h.traces, Clock: h.clock, IDs: &sequentialIDs{}, Actor: "test:operator",
	}
	return h
}

func sampleDefinition() BotDefinition {
	return BotDefinition{
		Tenant:       "engineering",
		Name:         "server-incident-investigator",
		Owner:        "platform-team",
		Mission:      "Diagnose server and application incidents",
		AllowedTools: []string{"observability.query_logs", "kubernetes.get_pods"},
		Model:        ModelPolicyDefinition{Model: "anthropic/claude-haiku-5.5", RoutingPolicy: "cost-optimized", MaxContextTokens: 32000},
		Budgets:      BudgetsDefinition{MaxModelCallsPerTask: 20, MaxToolCallsPerTask: 60, MaxDelegationDepth: 2, TaskTimeoutSeconds: 900},
	}
}

var errBoom = errors.New("boom")
