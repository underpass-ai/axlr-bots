package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func newestAudit(h *harness) domain.AuditEntry { return h.log.audits[len(h.log.audits)-1] }

func TestRegisterAndPatchCarryTheDefinitionVersion(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	definition := sampleDefinition()
	definition.Version = "1.0.0"
	bot, err := NewRegisterBot(h.deps).Execute(ctx, definition)
	if err != nil || bot.Version() != "1.0.0" || bot.Snapshot().Version != "1.0.0" {
		t.Fatalf("registered %q %v", bot.Version(), err)
	}
	if h.log.events[len(h.log.events)-1].Payload["version"] != "1.0.0" {
		t.Errorf("bot.registered = %+v", h.log.events[len(h.log.events)-1].Payload)
	}
	bad := sampleDefinition()
	bad.Name, bad.Version = "other", "1.0"
	if _, err := NewRegisterBot(h.deps).Execute(ctx, bad); err == nil {
		t.Error("a definition with a bad version was registered")
	}
	updated, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.1.0")})
	if err != nil || updated.Version() != "1.1.0" {
		t.Fatalf("patched %q %v", updated.Version(), err)
	}
	event := h.log.events[len(h.log.events)-1]
	if event.Type != domain.EventBotUpdated || event.Payload["previousVersion"] != "1.0.0" || event.Payload["version"] != "1.1.0" {
		t.Errorf("bot.updated = %+v", event.Payload)
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("one")}); err == nil {
		t.Error("a bad version was patched in")
	}
	cleared, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("")})
	if err != nil || !cleared.Version().IsZero() {
		t.Fatalf("cleared %q %v", cleared.Version(), err)
	}
}

func TestStageApplyAndUnstage(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	mission := "Watch the queue and say what fails."
	patch := BotPatch{Version: ptr("1.2.0"), Mission: &mission, Triggers: &[]domain.TriggerSnapshot{{Type: "manual", Name: "manual"}, {Type: "webhook", Name: "hook", Secret: "a-secret-of-sixteen-plus", Objective: "Look at what arrived"}}}

	staged, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), patch)
	if err != nil {
		t.Fatal(err)
	}
	held, ok := staged.Staged()
	if !ok || held.Version() != "1.2.0" || !held.StagedAt().Equal(t0) || staged.Mission() == domain.Mission(mission) || !staged.Version().IsZero() {
		t.Fatalf("staged %+v %v, mission %q", held, ok, staged.Mission())
	}
	stored, _ := h.bots.Get(ctx, bot.ID())
	if _, ok := stored.Staged(); !ok || stored.Mission() == domain.Mission(mission) {
		t.Fatal("the staged change is stored and nothing else changed")
	}
	if len(h.log.events) != 0 {
		t.Errorf("staging publishes no event yet: %+v", h.log.events)
	}
	audit := newestAudit(h)
	if audit.Action != "bot.stage" || audit.Outcome != domain.AuditOK || audit.Details["version"] != "1.2.0" || audit.Details["fields"] != "mission,triggers,version" {
		t.Errorf("audit = %+v", audit)
	}
	if _, replaced := audit.Details["replaced"]; replaced {
		t.Error("nothing was replaced")
	}

	view, err := NewGetStagedBot(h.deps).Execute(ctx, bot.ID())
	if err != nil || view.Version != "1.2.0" || view.Patch.Mission == nil || *view.Patch.Mission != mission {
		t.Fatalf("view %+v %v", view, err)
	}
	if secret := (*view.Patch.Triggers)[1].Secret; secret == "a-secret-of-sixteen-plus" {
		t.Error("the view shows the trigger's secret")
	}

	// A second stage replaces the first.
	h.clock.now = t0.Add(time.Minute)
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), patch); err != nil {
		t.Fatal(err)
	}
	if audit := newestAudit(h); audit.Details["replaced"] != "1.2.0" {
		t.Errorf("replacing says what it replaced: %+v", audit)
	}

	applied, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := applied.Staged(); ok || applied.Version() != "1.2.0" || applied.Mission() != domain.Mission(mission) {
		t.Fatalf("applied %q %q", applied.Version(), applied.Mission())
	}
	if hook, ok := applied.Trigger("hook"); !ok || hook.Snapshot().Secret != "a-secret-of-sixteen-plus" {
		t.Error("the staged trigger is applied with its secret")
	}
	stored, _ = h.bots.Get(ctx, bot.ID())
	if _, ok := stored.Staged(); ok || stored.Version() != "1.2.0" {
		t.Fatal("applying saves the change and drops the staged one in the same save")
	}
	updated := h.log.events[len(h.log.events)-1]
	if updated.Type != domain.EventBotUpdated || updated.Payload["version"] != "1.2.0" || updated.Payload["previousVersion"] != "" || updated.Payload["staged"] != "1.2.0" {
		t.Fatalf("event %v %+v", updated.Type, updated.Payload)
	}
	if a, b := h.log.audits[len(h.log.audits)-2], newestAudit(h); a.Action != "bot.update" || b.Action != "bot.staged.apply" || b.Outcome != domain.AuditOK {
		t.Errorf("audits %+v %+v", a, b)
	}

	// Nothing staged now: apply, unstage and show are refused.
	for name, try := range map[string]func() error{
		"apply":   func() error { _, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID()); return err },
		"unstage": func() error { _, err := NewUnstageBot(h.deps).Execute(ctx, bot.ID()); return err },
		"show":    func() error { _, err := NewGetStagedBot(h.deps).Execute(ctx, bot.ID()); return err },
	} {
		var state domain.StateError
		if err := try(); err == nil || !asState(err, &state) || !strings.Contains(err.Error(), "without a staged change") {
			t.Errorf("%s with nothing staged: %v", name, err)
		}
	}
	if audit := newestAudit(h); audit.Outcome != domain.AuditDenied {
		t.Errorf("a refused apply or unstage is audited: %+v", audit)
	}

	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.3.0"), Mission: &mission}); err != nil {
		t.Fatal(err)
	}
	unstaged, err := NewUnstageBot(h.deps).Execute(ctx, bot.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := unstaged.Staged(); ok || unstaged.Version() != "1.2.0" {
		t.Fatal("unstaging drops the change unapplied")
	}
	if audit := newestAudit(h); audit.Action != "bot.unstage" || audit.Outcome != domain.AuditOK || audit.Details["version"] != "1.3.0" {
		t.Errorf("audit = %+v", audit)
	}
}

func TestStageRefusesWhatTheUpdateWouldRefuse(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	level := "nonsense"
	cases := map[string]BotPatch{
		"no version":       {Mission: ptr("m")},
		"empty version":    {Version: ptr(""), Mission: ptr("m")},
		"bad version":      {Version: ptr("1.2"), Mission: ptr("m")},
		"bad level":        {Version: ptr("1.0.0"), Level: &level},
		"stranded rule":    {Version: ptr("1.0.0"), AllowedTools: &[]string{"kubernetes.get_pods"}, ToolRules: &domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "observability.query_logs"}}}},
		"version alone ok": {},
	}
	delete(cases, "version alone ok")
	for name, patch := range cases {
		if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), patch); err == nil {
			t.Errorf("%s was staged", name)
		} else if audit := newestAudit(h); audit.Action != "bot.stage" || audit.Outcome != domain.AuditDenied {
			t.Errorf("%s: audit %+v", name, audit)
		}
	}
	stored, _ := h.bots.Get(ctx, bot.ID())
	if _, ok := stored.Staged(); ok {
		t.Fatal("a refused stage holds nothing")
	}
	// A version alone is a change: the definition moves to it.
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.0.0")}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStageBot(h.deps).Execute(ctx, "nobody", BotPatch{Version: ptr("1.0.0")}); !domain.IsNotFound(err) {
		t.Errorf("an unknown bot: %v", err)
	}
	for name, try := range map[string]func() error{
		"apply":   func() error { _, err := NewApplyStagedBot(h.deps).Execute(ctx, "nobody"); return err },
		"unstage": func() error { _, err := NewUnstageBot(h.deps).Execute(ctx, "nobody"); return err },
		"show":    func() error { _, err := NewGetStagedBot(h.deps).Execute(ctx, "nobody"); return err },
	} {
		if err := try(); !domain.IsNotFound(err) {
			t.Errorf("%s of an unknown bot: %v", name, err)
		}
	}
}

// A staged change the bot no longer takes is refused at apply as the
// update refuses it, and stays staged; one that cannot be read as a
// patch is refused too.
func TestApplyRefusesAStagedChangeTheBotNoLongerTakes(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	rules := domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "observability.query_logs"}}}
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("2.0.0"), ToolRules: &rules}); err != nil {
		t.Fatal(err)
	}
	// Since then the tool the staged rule names left the allowed list.
	if _, err := NewUpdateBot(h.deps).Execute(ctx, bot.ID(), BotPatch{AllowedTools: &[]string{"kubernetes.get_pods"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID()); err == nil {
		t.Fatal("a staged rule naming a tool no longer allowed was applied")
	}
	if audit := newestAudit(h); audit.Action != "bot.staged.apply" || audit.Outcome != domain.AuditDenied || audit.Details["version"] != "2.0.0" {
		t.Errorf("audit = %+v", audit)
	}
	stored, _ := h.bots.Get(ctx, bot.ID())
	if _, ok := stored.Staged(); !ok {
		t.Fatal("a refused apply leaves the change staged")
	}

	unreadable, err := domain.NewStagedDefinition("2.1.0", t0, []byte(`{"version":"2.1.0","colour":"blue"}`))
	if err != nil {
		t.Fatal(err)
	}
	stored.Stage(unreadable)
	if err := h.bots.Save(ctx, stored); err != nil {
		t.Fatal(err)
	}
	for name, try := range map[string]func() error{
		"apply": func() error { _, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID()); return err },
		"show":  func() error { _, err := NewGetStagedBot(h.deps).Execute(ctx, bot.ID()); return err },
	} {
		if err := try(); err == nil || !strings.Contains(err.Error(), "cannot be read as a patch") {
			t.Errorf("%s of an unreadable change: %v", name, err)
		}
	}
}

func TestStagedWritersFailWithTheirStore(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.0.0")}); err != nil {
		t.Fatal(err)
	}
	h.bots.fail = errBoom
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.0.1")}); err != errBoom {
		t.Errorf("stage: %v", err)
	}
	if _, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID()); err != errBoom {
		t.Errorf("apply: %v", err)
	}
	if _, err := NewUnstageBot(h.deps).Execute(ctx, bot.ID()); err != errBoom {
		t.Errorf("unstage: %v", err)
	}
	h.bots.fail = nil
	h.log.recordFail = errBoom
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.0.1")}); err != errBoom {
		t.Errorf("stage without its audit: %v", err)
	}
	if _, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID()); err != errBoom {
		t.Errorf("apply without its audit: %v", err)
	}
	h.log.recordFail = nil
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.0.2")}); err != nil {
		t.Fatal(err)
	}
	h.log.recordFail = errBoom
	if _, err := NewUnstageBot(h.deps).Execute(ctx, bot.ID()); err != errBoom {
		t.Errorf("unstage without its audit: %v", err)
	}
	h.log.recordFail = nil
	if _, err := NewStageBot(h.deps).Execute(ctx, bot.ID(), BotPatch{Version: ptr("1.0.3")}); err != nil {
		t.Fatal(err)
	}
	h.log.fail = errBoom
	if _, err := NewApplyStagedBot(h.deps).Execute(ctx, bot.ID()); err != errBoom {
		t.Errorf("apply without its event: %v", err)
	}
}

func TestSerialRunnerStagedWriters(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	runner := NewSerialRunner(h.deps)
	if _, err := runner.StageBot(ctx, bot.ID(), BotPatch{Version: ptr("1.0.0")}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.UnstageBot(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.StageBot(ctx, bot.ID(), BotPatch{Version: ptr("1.0.0")}); err != nil {
		t.Fatal(err)
	}
	if applied, err := runner.ApplyStagedBot(ctx, bot.ID()); err != nil || applied.Version() != "1.0.0" {
		t.Fatalf("applied %q %v", applied.Version(), err)
	}
}

func asState(err error, target *domain.StateError) bool {
	state, ok := err.(domain.StateError)
	*target = state
	return ok
}
