package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestListTasksFiltersAndOrdersNewestFirst(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	first := submit(t, h, bot, "k-1").Task
	h.clock.now = t0.Add(time.Minute)
	second := submit(t, h, bot, "k-2").Task
	h.clock.now = t0.Add(2 * time.Minute)
	third := submit(t, h, bot, "k-3").Task
	if _, err := NewCancelTask(h.deps).Execute(ctx, third.ID()); err != nil {
		t.Fatal(err)
	}
	all, err := NewListTasks(h.deps).Execute(ctx, TaskQuery{})
	if err != nil || len(all) != 3 || all[0].ID() != third.ID() || all[2].ID() != first.ID() {
		t.Fatalf("all = %v %v", ids(all), err)
	}
	for name, tc := range map[string]struct {
		query TaskQuery
		want  []domain.TaskID
	}{
		"by bot":    {TaskQuery{BotID: bot.ID().String()}, []domain.TaskID{third.ID(), second.ID(), first.ID()}},
		"by status": {TaskQuery{Status: "Pending"}, []domain.TaskID{second.ID(), first.ID()}},
		"since":     {TaskQuery{Since: t0.Add(30 * time.Second)}, []domain.TaskID{third.ID(), second.ID()}},
		"limit":     {TaskQuery{Limit: 1}, []domain.TaskID{third.ID()}},
		"capped":    {TaskQuery{Limit: MaxTaskLimit + 5}, []domain.TaskID{third.ID(), second.ID(), first.ID()}},
	} {
		got, err := NewListTasks(h.deps).Execute(ctx, tc.query)
		if err != nil || len(got) != len(tc.want) {
			t.Errorf("%s: %v %v", name, ids(got), err)
			continue
		}
		for i := range got {
			if got[i].ID() != tc.want[i] {
				t.Errorf("%s: %v, want %v", name, ids(got), tc.want)
				break
			}
		}
	}
	for name, query := range map[string]TaskQuery{
		"bad bot":        {BotID: "Bad Bot"},
		"unknown bot":    {BotID: "nobody"},
		"bad status":     {Status: "Sleeping"},
		"negative limit": {Limit: -1},
	} {
		if _, err := NewListTasks(h.deps).Execute(ctx, query); err == nil {
			t.Errorf("%s: listed", name)
		}
	}
	h.tasks.listFail = errBoom
	if _, err := NewListTasks(h.deps).Execute(ctx, TaskQuery{}); !errors.Is(err, errBoom) {
		t.Errorf("list failure = %v", err)
	}
}

func ids(tasks []domain.Task) []domain.TaskID {
	out := make([]domain.TaskID, len(tasks))
	for i, task := range tasks {
		out[i] = task.ID()
	}
	return out
}

func TestPauseBotHoldsAnEnabledBot(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	paused, err := NewPauseBot(h.deps).Execute(ctx, bot.ID())
	if err != nil || paused.Status() != domain.BotPaused {
		t.Fatalf("paused = %s %v", paused.Status(), err)
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.paused" {
		t.Errorf("events = %v", got)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "bot.pause" || h.log.audits[0].Outcome != domain.AuditOK {
		t.Errorf("audits = %+v", h.log.audits)
	}
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "x", IdempotencyKey: "k"}); err == nil {
		t.Error("a paused bot accepted a task")
	}
	if _, err := NewPauseBot(h.deps).Execute(ctx, bot.ID()); err == nil {
		t.Error("a paused bot paused again")
	}
	if h.log.audits[len(h.log.audits)-1].Outcome != domain.AuditDenied {
		t.Errorf("refusal not audited: %+v", h.log.audits)
	}
	if _, err := NewPauseBot(h.deps).Execute(ctx, "nobody"); !domain.IsNotFound(err) {
		t.Errorf("unknown bot = %v", err)
	}
	enabled, err := NewEnableBot(h.deps).Execute(ctx, bot.ID())
	if err != nil || enabled.Status() != domain.BotEnabled {
		t.Errorf("enable after pause = %s %v", enabled.Status(), err)
	}
	h.bots.fail = errBoom
	if _, err := NewPauseBot(h.deps).Execute(ctx, bot.ID()); !errors.Is(err, errBoom) {
		t.Errorf("save failure = %v", err)
	}
	h.bots.fail = nil
	h.log.fail = errBoom
	if _, err := NewPauseBot(h.deps).Execute(ctx, bot.ID()); !errors.Is(err, errBoom) {
		t.Errorf("event failure = %v", err)
	}
}

func TestGetOverviewCountsNowAndSince(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	old := submit(t, h, bot, "old").Task
	h.runtime.report.Usage = domain.Usage{Requests: 1, CostUSD: 0.25}
	if _, err := NewRunTask(h.deps).Execute(ctx, old.ID()); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(time.Hour)
	fresh := submit(t, h, bot, "fresh").Task
	h.runtime.report.Usage = domain.Usage{Requests: 2, CostUSD: 0.5}
	if _, err := NewRunTask(h.deps).Execute(ctx, fresh.ID()); err != nil {
		t.Fatal(err)
	}
	submit(t, h, bot, "pending")
	node, err := domain.NewNode("web-1", "web-1", nil, domain.NodeCapabilities{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.Save(ctx, node); err != nil {
		t.Fatal(err)
	}
	overview, err := NewGetOverview(h.deps).Execute(ctx, t0.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if overview.Nodes["Registered"] != 1 || overview.Nodes["Online"] != 0 || overview.Bots["Enabled"] != 1 || overview.Bots["Paused"] != 0 {
		t.Errorf("nodes and bots = %+v %+v", overview.Nodes, overview.Bots)
	}
	if overview.Tasks["Completed"] != 2 || overview.Tasks["Pending"] != 1 || overview.TasksSince["Completed"] != 1 || overview.TasksSince["Pending"] != 1 || overview.TasksSince["Failed"] != 0 {
		t.Errorf("tasks = %+v since %+v", overview.Tasks, overview.TasksSince)
	}
	if overview.ExecutionsSince != 1 || overview.CostUSDSince != 0.5 || !overview.Since.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("executions = %d cost %v since %v", overview.ExecutionsSince, overview.CostUSDSince, overview.Since)
	}
	today, err := NewGetOverview(h.deps).Execute(ctx, time.Time{})
	if err != nil || !today.Since.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) || today.ExecutionsSince != 2 || today.CostUSDSince != 0.75 {
		t.Errorf("today = %+v %v", today, err)
	}
	for name, breakIt := range map[string]func(){
		"nodes":      func() { h.nodes.listFail = errBoom },
		"tasks":      func() { h.tasks.listFail = errBoom },
		"executions": func() { h.execs.listFail = errBoom },
	} {
		h.nodes.listFail, h.tasks.listFail, h.execs.listFail = nil, nil, nil
		breakIt()
		if _, err := NewGetOverview(h.deps).Execute(ctx, time.Time{}); !errors.Is(err, errBoom) {
			t.Errorf("%s failure = %v", name, err)
		}
	}
}

func TestListTaskEventsIsTheTaskTimeline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k-1").Task
	other := submit(t, h, bot, "k-2").Task
	if _, err := NewRunTask(h.deps).Execute(ctx, task.ID()); err != nil {
		t.Fatal(err)
	}
	events, err := NewListTaskEvents(h.deps).Execute(ctx, task.ID(), 0)
	if err != nil || len(events) != 3 || events[0].Type != domain.EventTaskSubmitted || events[2].Type != domain.EventTaskCompleted {
		t.Fatalf("events = %+v %v", events, err)
	}
	for _, e := range events {
		if e.TaskID != task.ID() {
			t.Errorf("event of another task: %+v", e)
		}
	}
	last, err := NewListTaskEvents(h.deps).Execute(ctx, task.ID(), 1)
	if err != nil || len(last) != 1 || last[0].Type != domain.EventTaskCompleted {
		t.Errorf("last = %+v %v", last, err)
	}
	capped, err := NewListTaskEvents(h.deps).Execute(ctx, other.ID(), MaxEventLimit+1)
	if err != nil || len(capped) != 1 {
		t.Errorf("other = %+v %v", capped, err)
	}
	if _, err := NewListTaskEvents(h.deps).Execute(ctx, "task-none", 0); !domain.IsNotFound(err) {
		t.Errorf("unknown task = %v", err)
	}
	h.log.events = nil
	empty, err := NewListTaskEvents(h.deps).Execute(ctx, task.ID(), 0)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("empty = %#v %v", empty, err)
	}
	h.log.fail = errBoom
	if _, err := NewListTaskEvents(h.deps).Execute(ctx, task.ID(), 0); !errors.Is(err, errBoom) {
		t.Errorf("reader failure = %v", err)
	}
}

func TestRecordNodeCertificateKeepsTheCurrentExpiry(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, err := domain.NewNode("web-1", "web-1", nil, domain.NodeCapabilities{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Certify("serial-1", nil, t0); err != nil {
		t.Fatal(err)
	}
	if err := h.nodes.Save(ctx, node); err != nil {
		t.Fatal(err)
	}
	expiry := t0.Add(90 * 24 * time.Hour)
	uc := NewRecordNodeCertificate(h.deps)
	if err := uc.Execute(ctx, "web-1", "serial-1", expiry); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.nodes.Get(ctx, "web-1")
	if !stored.CertificateExpiry().Equal(expiry) || stored.Snapshot().CertificateNotAfter == nil {
		t.Errorf("expiry = %v", stored.CertificateExpiry())
	}
	if err := uc.Execute(ctx, "web-1", "serial-old", t0); err != nil {
		t.Fatal(err)
	}
	stored, _ = h.nodes.Get(ctx, "web-1")
	if !stored.CertificateExpiry().Equal(expiry) {
		t.Errorf("an old certificate changed the expiry: %v", stored.CertificateExpiry())
	}
	if err := uc.Execute(ctx, "nobody", "serial-1", expiry); err != nil {
		t.Errorf("unknown node = %v", err)
	}
	h.nodes.fail = errBoom
	if err := uc.Execute(ctx, "web-1", "serial-1", expiry.Add(time.Hour)); !errors.Is(err, errBoom) {
		t.Errorf("save failure = %v", err)
	}
	if err := uc.Execute(ctx, "web-1", "serial-1", expiry); err != nil {
		t.Errorf("an unchanged expiry saved: %v", err)
	}
}
