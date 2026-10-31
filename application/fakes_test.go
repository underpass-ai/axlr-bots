package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

var t0 = time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)

type memoryBots struct {
	mu       sync.Mutex
	bots     map[domain.BotID]domain.Bot
	fail     error
	listFail error
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
	if m.listFail != nil {
		return nil, m.listFail
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.Bot, 0, len(m.bots))
	for _, b := range m.bots {
		out = append(out, b)
	}
	return out, nil
}

type memoryTasks struct {
	tasks    map[domain.TaskID]domain.Task
	fail     error
	listFail error
	saves    int
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
	if m.listFail != nil {
		return nil, m.listFail
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

func (m *memoryTasks) List(context.Context) ([]domain.Task, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	out := make([]domain.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t)
	}
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

type memoryIncidents struct {
	incidents map[domain.IncidentID]domain.Incident
	fail      error
	listFail  error
	saves     int
}

func (m *memoryIncidents) Save(_ context.Context, incident domain.Incident) error {
	if m.fail != nil {
		return m.fail
	}
	if m.incidents == nil {
		m.incidents = map[domain.IncidentID]domain.Incident{}
	}
	m.incidents[incident.ID()] = incident
	m.saves++
	return nil
}

func (m *memoryIncidents) Get(_ context.Context, id domain.IncidentID) (domain.Incident, error) {
	incident, ok := m.incidents[id]
	if !ok {
		return domain.Incident{}, domain.NotFoundError{Kind: "incident", ID: id.String()}
	}
	return incident, nil
}

func (m *memoryIncidents) FindNewestByGroupKey(_ context.Context, tenant domain.TenantID, groupKey string) (domain.Incident, bool, error) {
	if m.listFail != nil {
		return domain.Incident{}, false, m.listFail
	}
	var newest domain.Incident
	found := false
	for _, incident := range m.incidents {
		if incident.TenantID() == tenant && incident.GroupKey() == groupKey && (!found || incident.OpenedAt().After(newest.OpenedAt())) {
			newest, found = incident, true
		}
	}
	return newest, found, nil
}

func (m *memoryIncidents) List(context.Context) ([]domain.Incident, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	out := make([]domain.Incident, 0, len(m.incidents))
	for _, incident := range m.incidents {
		out = append(out, incident)
	}
	return out, nil
}

type memoryExecutions struct {
	executions map[domain.ExecutionID]domain.Execution
	fail       error
	listFail   error
	// beforeSave, when set, runs before each Save with the execution
	// about to be saved: a write by someone else between a use case's
	// read and its save. The hook clears it when it has done its write.
	beforeSave func(domain.Execution)
}

func (m *memoryExecutions) Save(_ context.Context, e domain.Execution) error {
	if hook := m.beforeSave; hook != nil {
		hook(e)
	}
	if m.fail != nil {
		return m.fail
	}
	// The file store's contract: an execution that ended never runs again.
	if stored, ok := m.executions[e.ID()]; ok && stored.Outcome().IsFinal() && e.Outcome() == domain.ExecutionRunning {
		return domain.StateError{Subject: "execution " + e.ID().String(), Current: stored.Outcome().String(), Attempted: "run again"}
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

func (m *memoryExecutions) ListRunning(context.Context) ([]domain.Execution, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	var out []domain.Execution
	for _, e := range m.executions {
		if e.Outcome() == domain.ExecutionRunning {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b domain.Execution) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

func (m *memoryExecutions) List(context.Context) ([]domain.Execution, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	out := make([]domain.Execution, 0, len(m.executions))
	for _, e := range m.executions {
		out = append(out, e)
	}
	return out, nil
}

type memoryNodes struct {
	nodes    map[domain.NodeID]domain.Node
	fail     error
	listFail error
}

func (m *memoryNodes) Save(_ context.Context, node domain.Node) error {
	if m.fail != nil {
		return m.fail
	}
	if m.nodes == nil {
		m.nodes = map[domain.NodeID]domain.Node{}
	}
	m.nodes[node.ID()] = node
	return nil
}

func (m *memoryNodes) Get(_ context.Context, id domain.NodeID) (domain.Node, error) {
	node, ok := m.nodes[id]
	if !ok {
		return domain.Node{}, domain.NotFoundError{Kind: "node", ID: id.String()}
	}
	return node, nil
}

func (m *memoryNodes) List(context.Context) ([]domain.Node, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	out := make([]domain.Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, n)
	}
	slices.SortFunc(out, func(a, b domain.Node) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

type collector struct {
	events     []domain.Event
	audits     []domain.AuditEntry
	fail       error
	recordFail error
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
	if c.recordFail != nil {
		return c.recordFail
	}
	c.audits = append(c.audits, a)
	return nil
}

// ListEvents is the EventReader over what was published.
func (c *collector) ListEvents(_ context.Context, bot domain.BotID, limit int) ([]domain.Event, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	var out []domain.Event
	for _, e := range c.events {
		if e.BotID == bot {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// ListTeamEvents is the EventReader of a team over what was published.
func (c *collector) ListTeamEvents(_ context.Context, tenant domain.TenantID, team domain.TeamName, bots []domain.BotID, limit int) ([]domain.Event, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	var out []domain.Event
	for _, e := range c.events {
		if e.BotID == "" && e.GroupName == team.String() && e.TenantID == tenant {
			out = append(out, e)
			continue
		}
		for _, bot := range bots {
			if e.BotID == bot {
				out = append(out, e)
				break
			}
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// ListTaskEvents is the EventReader of a task over what was published.
func (c *collector) ListTaskEvents(_ context.Context, task domain.TaskID, limit int) ([]domain.Event, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	var out []domain.Event
	for _, e := range c.events {
		if e.TaskID == task {
			out = append(out, e)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// has reports an event of the type about the task.
func (c *collector) has(kind string, task domain.TaskID) bool {
	for _, e := range c.events {
		if e.Type.String() == kind && e.TaskID == task {
			return true
		}
	}
	return false
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

// fakeDecisions is the fake DecisionExecutor: it records requests and
// answers with a scripted report.
type fakeDecisions struct {
	requests []DecisionRequest
	report   ExecutionReport
	err      error
}

func (f *fakeDecisions) Decide(_ context.Context, request DecisionRequest) (ExecutionReport, error) {
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

// memoryArtifacts is the fake ArtifactStore: the reports it was given
// and the artifacts kept per execution.
type memoryArtifacts struct {
	reports   map[domain.TaskID]string
	fail      error
	kept      map[domain.ExecutionID]map[string]storedBytes
	saveFail  error
	listFail  error
	openFail  error
	saveCalls int
}

type storedBytes struct {
	listing domain.Artifact
	content []byte
}

func (m *memoryArtifacts) SaveArtifact(_ context.Context, execution domain.ExecutionID, artifact domain.Artifact, content io.Reader) error {
	m.saveCalls++
	if m.saveFail != nil {
		return m.saveFail
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	if m.kept == nil {
		m.kept = map[domain.ExecutionID]map[string]storedBytes{}
	}
	if m.kept[execution] == nil {
		m.kept[execution] = map[string]storedBytes{}
	}
	m.kept[execution][artifact.Name] = storedBytes{listing: artifact, content: data}
	return nil
}

func (m *memoryArtifacts) StoredArtifacts(_ context.Context, execution domain.ExecutionID) ([]domain.Artifact, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	var out []domain.Artifact
	for _, kept := range m.kept[execution] {
		out = append(out, kept.listing)
	}
	slices.SortFunc(out, func(a, b domain.Artifact) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (m *memoryArtifacts) OpenArtifact(_ context.Context, execution domain.ExecutionID, name string) (io.ReadCloser, error) {
	if m.openFail != nil {
		return nil, m.openFail
	}
	kept, ok := m.kept[execution][name]
	if !ok {
		return nil, domain.NotFoundError{Kind: "artifact", ID: name}
	}
	return io.NopCloser(bytes.NewReader(kept.content)), nil
}

// fakeWorkspace is the fake Workspace: the files of each bot's
// workspace by reference.
type fakeWorkspace struct {
	files map[string]string
	fail  error
}

func (f *fakeWorkspace) OpenDelivered(bot domain.BotID, reference string) (io.ReadCloser, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	content, ok := f.files[bot.String()+"/"+reference]
	if !ok {
		return nil, fmt.Errorf("%s: no such file in the workspace of %s", reference, bot)
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

// memoryDeliveries is the fake DeliveryRepository.
type memoryDeliveries struct {
	deliveries map[domain.DeliveryID]domain.Delivery
	fail       error
	getFail    error
	listFail   error
}

func (m *memoryDeliveries) Save(_ context.Context, d domain.Delivery) error {
	if m.fail != nil {
		return m.fail
	}
	if m.deliveries == nil {
		m.deliveries = map[domain.DeliveryID]domain.Delivery{}
	}
	m.deliveries[d.ID()] = d
	return nil
}

func (m *memoryDeliveries) Get(_ context.Context, id domain.DeliveryID) (domain.Delivery, error) {
	if m.getFail != nil {
		return domain.Delivery{}, m.getFail
	}
	d, ok := m.deliveries[id]
	if !ok {
		return domain.Delivery{}, domain.NotFoundError{Kind: "delivery", ID: id.String()}
	}
	return d, nil
}

func (m *memoryDeliveries) ListPending(context.Context) ([]domain.Delivery, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	var out []domain.Delivery
	for _, d := range m.deliveries {
		if d.Status() == domain.DeliveryPending {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b domain.Delivery) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

// fakeNotifier is the fake NotificationSender: it records each attempt
// and fails the first failures of them.
type fakeNotifier struct {
	sent     []domain.Delivery
	targets  []domain.NotifyTarget
	failures int
}

func (f *fakeNotifier) Send(_ context.Context, target domain.NotifyTarget, d domain.Delivery) error {
	f.sent = append(f.sent, d)
	f.targets = append(f.targets, target)
	if f.failures > 0 {
		f.failures--
		return errors.New("HTTP 503")
	}
	return nil
}

func testWait(since time.Time) domain.ApprovalWait {
	return domain.ApprovalWait{CallID: "call-1", Tool: "local_write", Since: since, Deadline: since.Add(time.Hour), OnExpire: domain.ExpireDeny}
}

func (m *memoryArtifacts) SaveReport(_ context.Context, task domain.TaskID, execution domain.ExecutionID, content []byte) (string, error) {
	if m.fail != nil {
		return "", m.fail
	}
	if m.reports == nil {
		m.reports = map[domain.TaskID]string{}
	}
	m.reports[task] = string(content)
	return "/artifacts/" + task.String() + "/" + execution.String() + "/report.md", nil
}

func (m *memoryArtifacts) LoadReport(_ context.Context, task domain.TaskID) ([]byte, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	report, ok := m.reports[task]
	if !ok {
		return nil, domain.NotFoundError{Kind: "report", ID: task.String()}
	}
	return []byte(report), nil
}

type memoryTimers struct {
	timers map[string]domain.Timer
	fail   error
}

func (m *memoryTimers) Save(_ context.Context, timer domain.Timer) error {
	if m.fail != nil {
		return m.fail
	}
	if m.timers == nil {
		m.timers = map[string]domain.Timer{}
	}
	m.timers[timer.Key()] = timer
	return nil
}

func (m *memoryTimers) Get(_ context.Context, key string) (domain.Timer, error) {
	timer, ok := m.timers[key]
	if !ok {
		return domain.Timer{}, domain.NotFoundError{Kind: "timer", ID: key}
	}
	return timer, nil
}

func (m *memoryTimers) List(context.Context) ([]domain.Timer, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]domain.Timer, 0, len(m.timers))
	for _, t := range m.timers {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b domain.Timer) int { return strings.Compare(a.Key(), b.Key()) })
	return out, nil
}

func (m *memoryTimers) Delete(_ context.Context, key string) error {
	if m.fail != nil {
		return m.fail
	}
	delete(m.timers, key)
	return nil
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
	incs    *memoryIncidents
	execs   *memoryExecutions
	timers  *memoryTimers
	nodes   *memoryNodes
	enrol   *memoryEnrollments
	ca      *fakeCertifier
	deny    *memoryDenylist
	log     *collector
	runtime *fakeRuntime
	decider *fakeDecisions
	traces  *fakeTraces
	store   *memoryArtifacts
	work    *fakeWorkspace
	outbox  *memoryDeliveries
	notify  *fakeNotifier
	clock   *fixedClock
}

func newHarness() *harness {
	h := &harness{
		bots:    &memoryBots{},
		tasks:   &memoryTasks{},
		incs:    &memoryIncidents{},
		execs:   &memoryExecutions{},
		timers:  &memoryTimers{},
		nodes:   &memoryNodes{},
		enrol:   &memoryEnrollments{},
		ca:      &fakeCertifier{},
		deny:    &memoryDenylist{},
		log:     &collector{},
		runtime: &fakeRuntime{report: ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "sess-1"}, TracePath: "/traces/exec-1.jsonl"}},
		decider: &fakeDecisions{report: ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done after approval", SessionID: "sess-1"}, TracePath: "/traces/exec-2.jsonl"}},
		traces:  &fakeTraces{paths: map[domain.ExecutionID]string{}},
		store:   &memoryArtifacts{},
		work:    &fakeWorkspace{files: map[string]string{}},
		outbox:  &memoryDeliveries{},
		notify:  &fakeNotifier{},
		clock:   &fixedClock{now: t0},
	}
	h.deps = Dependencies{
		Bots: h.bots, Tasks: h.tasks, Incidents: h.incs, Executions: h.execs, Timers: h.timers, Nodes: h.nodes, Enrollments: h.enrol, Certifier: h.ca, Denylist: h.deny, Events: h.log, EventReader: h.log, Audit: h.log,
		Runtime: h.runtime, Decisions: h.decider, Traces: h.traces, Artifacts: h.store, Workspace: h.work, Deliveries: h.outbox, Notifier: h.notify,
		Clock: h.clock, IDs: &sequentialIDs{}, Actor: "test:operator",
	}
	return h
}

func sampleDefinition() BotDefinition {
	return BotDefinition{
		Tenant:       "engineering",
		Name:         "server-incident-investigator",
		Owner:        "platform-team",
		Mission:      "Do what each task asks, within the policy",
		AllowedTools: []string{"observability.query_logs", "kubernetes.get_pods"},
		Model:        ModelPolicyDefinition{Model: "anthropic/claude-haiku-5.5", RoutingPolicy: "cost-optimized", MaxContextTokens: 32000},
		Budgets:      BudgetsDefinition{MaxModelCallsPerTask: 20, MaxToolCallsPerTask: 60, MaxDelegationDepth: 2, TaskTimeoutSeconds: 900},
	}
}

var errBoom = errors.New("boom")

type memoryEnrollments struct {
	enrollments map[domain.EnrollmentID]domain.Enrollment
	fail        error
	listFail    error
}

func (m *memoryEnrollments) Save(_ context.Context, e domain.Enrollment) error {
	if m.fail != nil {
		return m.fail
	}
	if m.enrollments == nil {
		m.enrollments = map[domain.EnrollmentID]domain.Enrollment{}
	}
	m.enrollments[e.ID()] = e
	return nil
}

func (m *memoryEnrollments) Get(_ context.Context, id domain.EnrollmentID) (domain.Enrollment, error) {
	if m.listFail != nil {
		return domain.Enrollment{}, m.listFail
	}
	e, ok := m.enrollments[id]
	if !ok {
		return domain.Enrollment{}, domain.NotFoundError{Kind: "enrollment", ID: id.String()}
	}
	return e, nil
}

func (m *memoryEnrollments) List(context.Context) ([]domain.Enrollment, error) {
	if m.listFail != nil {
		return nil, m.listFail
	}
	out := make([]domain.Enrollment, 0, len(m.enrollments))
	for _, e := range m.enrollments {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b domain.Enrollment) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

// fakeCertifier is the fake Certifier: it issues a serial per request
// and remembers what it was asked.
type fakeCertifier struct {
	issued  int
	fail    error
	caFail  error
	lastCSR []byte
	lastID  domain.NodeID
}

func (f *fakeCertifier) IssueNodeCertificate(_ context.Context, node domain.NodeID, csr []byte) (IssuedCertificate, error) {
	if f.fail != nil {
		return IssuedCertificate{}, f.fail
	}
	f.issued++
	f.lastCSR, f.lastID = csr, node
	return IssuedCertificate{CertificatePEM: []byte("CERT " + node.String()), Serial: fmt.Sprintf("serial-%d", f.issued), NotAfter: t0.Add(24 * time.Hour)}, nil
}

func (f *fakeCertifier) CACertificate(context.Context) ([]byte, error) {
	if f.caFail != nil {
		return nil, f.caFail
	}
	return []byte("CA PEM"), nil
}

type memoryDenylist struct {
	serials map[string]string
	fail    error
}

func (m *memoryDenylist) Deny(_ context.Context, serial, node string) error {
	if m.fail != nil {
		return m.fail
	}
	if m.serials == nil {
		m.serials = map[string]string{}
	}
	m.serials[serial] = node
	return nil
}

func (m *memoryDenylist) IsDenied(_ context.Context, serial string) (bool, error) {
	if m.fail != nil {
		return false, m.fail
	}
	_, denied := m.serials[serial]
	return denied, nil
}
