package application

import (
	"context"
	"errors"
	"testing"

	"github.com/underpass-ai/underpass-bots/domain"
)

func TestDependenciesValidate(t *testing.T) {
	h := newHarness()
	if err := h.deps.Validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Dependencies){
		func(d *Dependencies) { d.Bots = nil },
		func(d *Dependencies) { d.Tasks = nil },
		func(d *Dependencies) { d.Executions = nil },
		func(d *Dependencies) { d.Events = nil },
		func(d *Dependencies) { d.Audit = nil },
		func(d *Dependencies) { d.Runtime = nil },
		func(d *Dependencies) { d.Decisions = nil },
		func(d *Dependencies) { d.Traces = nil },
		func(d *Dependencies) { d.Clock = nil },
		func(d *Dependencies) { d.IDs = nil },
		func(d *Dependencies) { d.Actor = "" },
	}
	for i, m := range mutations {
		d := h.deps
		m(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("mutation %d validated", i)
		}
	}
}

func TestRegisterBot(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot, err := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if bot.ID() != "server-incident-investigator" || bot.Status() != domain.BotRegistered {
		t.Errorf("bot = %s %s", bot.ID(), bot.Status())
	}
	stored, err := h.bots.Get(ctx, bot.ID())
	if err != nil || stored.Status() != domain.BotRegistered {
		t.Errorf("stored: %v %s", err, stored.Status())
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "bot.registered" {
		t.Errorf("events = %v", got)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "bot.register" || h.log.audits[0].Actor != "test:operator" || h.log.audits[0].Outcome != domain.AuditOK {
		t.Errorf("audits = %+v", h.log.audits)
	}
	if h.log.events[0].SchemaVersion != domain.EventSchemaV1 || h.log.events[0].TenantID != "engineering" {
		t.Errorf("event = %+v", h.log.events[0])
	}

	// Same id again: conflict, audited as denied.
	if _, err := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition()); err == nil {
		t.Fatal("duplicate accepted")
	} else {
		var conflict domain.ConflictError
		if !errors.As(err, &conflict) {
			t.Errorf("want ConflictError, got %v", err)
		}
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied {
		t.Errorf("denied audit missing: %+v", last)
	}

	// Explicit id wins over the name.
	withID := sampleDefinition()
	withID.ID = "sii"
	withID.Name = "Server Incident Investigator"
	if bot, err := NewRegisterBot(h.deps).Execute(ctx, withID); err != nil || bot.ID() != "sii" {
		t.Errorf("explicit id: %v %s", err, bot.ID())
	}
}

func TestRegisterBotRejectsBadDefinitions(t *testing.T) {
	h := newHarness()
	bad := []func(*BotDefinition){
		func(d *BotDefinition) { d.Name = "Needs Lowercase Id" },
		func(d *BotDefinition) { d.Tenant = "" },
		func(d *BotDefinition) { d.Name = "" },
		func(d *BotDefinition) { d.Owner = "" },
		func(d *BotDefinition) { d.Mission = "" },
		func(d *BotDefinition) { d.AllowedTools = []string{"Bad Tool"} },
		func(d *BotDefinition) { d.Model.RoutingPolicy = "fixed"; d.Model.Model = "" },
		func(d *BotDefinition) { d.Budgets.TaskTimeoutSeconds = -1 },
	}
	for i, mutate := range bad {
		d := sampleDefinition()
		mutate(&d)
		if _, err := NewRegisterBot(h.deps).Execute(context.Background(), d); err == nil {
			t.Errorf("bad definition %d accepted", i)
		}
	}
	if len(h.log.events) != 0 {
		t.Error("bad definitions published events")
	}
}

func TestRegisterBotPropagatesPortErrors(t *testing.T) {
	ctx := context.Background()
	h := newHarness()
	h.bots.fail = errBoom
	if _, err := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition()); !errors.Is(err, errBoom) {
		t.Errorf("save error: %v", err)
	}
	h = newHarness()
	h.log.fail = errBoom
	if _, err := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition()); !errors.Is(err, errBoom) {
		t.Errorf("publish error: %v", err)
	}
}

func TestEnableGetListBot(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	registered, _ := NewRegisterBot(h.deps).Execute(ctx, sampleDefinition())

	if _, err := NewEnableBot(h.deps).Execute(ctx, "missing"); !domain.IsNotFound(err) {
		t.Errorf("missing bot: %v", err)
	}
	enabled, err := NewEnableBot(h.deps).Execute(ctx, registered.ID())
	if err != nil || enabled.Status() != domain.BotEnabled {
		t.Fatalf("enable: %v", err)
	}
	if got := h.log.eventTypes(); len(got) != 2 || got[1] != "bot.enabled" {
		t.Errorf("events = %v", got)
	}
	// Enabling twice is a state error, audited as denied.
	if _, err := NewEnableBot(h.deps).Execute(ctx, registered.ID()); err == nil {
		t.Error("second enable accepted")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied {
		t.Errorf("denied audit missing: %+v", last)
	}

	got, err := NewGetBot(h.deps).Execute(ctx, registered.ID())
	if err != nil || got.Status() != domain.BotEnabled {
		t.Errorf("get: %v", err)
	}
	if _, err := NewGetBot(h.deps).Execute(ctx, "nope"); !domain.IsNotFound(err) {
		t.Errorf("get missing: %v", err)
	}
	list, err := NewListBots(h.deps).Execute(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("list: %v %d", err, len(list))
	}

	h.bots.fail = errBoom
	if _, err := NewEnableBot(h.deps).Execute(ctx, registered.ID()); err == nil {
		t.Error("save error swallowed")
	}
	h.bots.fail = nil
	h.log.fail = errBoom
	paused := registered
	_ = paused.Enable(t0)
	_ = paused.Pause(t0)
	_ = h.bots.Save(ctx, paused)
	if _, err := NewEnableBot(h.deps).Execute(ctx, registered.ID()); !errors.Is(err, errBoom) {
		t.Errorf("publish error: %v", err)
	}
}
