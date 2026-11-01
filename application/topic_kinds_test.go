package application

import (
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// The checks of PR 2 of the publish/subscribe ADR: the name's kind fixes
// the severity, a bot never emits the control plane's kinds, and an
// older node's severity is ignored.
func TestTheTopicsKindFixesTheSeverity(t *testing.T) {
	h := newHarness()
	parent := portTeam(t, h)
	turno := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.turno", Message: "shift change"})
	if turno.Refused != nil || turno.Severity != domain.SeverityInfo || len(turno.Deliveries) != 0 || len(turno.Skipped) != 0 {
		t.Errorf("info.turno = %+v", turno)
	}
	if event, ok := lastEvent(h, domain.EventEventEmitted); !ok || event.Payload["severity"] != "info" || event.Payload["name"] != "info.turno" {
		t.Errorf("emitted = %+v", event)
	}
	if answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.recall", Message: "come back", To: []string{"quad-1"}}); answer.Severity != domain.SeverityCritical || len(answer.Deliveries) != 1 || !answer.Deliveries[0].Urgent {
		t.Errorf("stop.recall = %+v", answer)
	}
	old := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "info.x", Message: "an older node", Severity: "critical", To: []string{"quad-2"}})
	if old.Refused != nil || old.Severity != domain.SeverityInfo || len(old.Deliveries) != 1 || old.Deliveries[0].Urgent {
		t.Errorf("an older node's severity = %+v", old)
	}
	refused := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "error.bot", Message: "x"})
	if refused.Refused == nil || refused.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("error.bot = %+v", refused)
	}
	if event, ok := lastEvent(h, domain.EventEventRefused); !ok || event.Payload["name"] != "error.bot" || event.Payload["reason"] != domain.RefusedNotAllowed {
		t.Errorf("refused = %+v", event)
	}
}

func TestAnEmitRulesMaxSeverityIsIgnored(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	warner := teamBot(t, h, "warner", []string{"port"}, func(d *BotDefinition) {
		d.Events = &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "warn.*", MaxSeverity: "critical"}}}
	})
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	run := runOnNode(t, h, warner, "web-1", "ask-1")
	if names := run.Run.Events.Names; len(names) != 1 || names[0] != "warn.*" {
		t.Errorf("offered names = %v", names)
	}
	warn := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "warn.x", Message: "x"})
	if warn.Refused != nil || warn.Severity != domain.SeverityWarning {
		t.Errorf("warn.x = %+v", warn)
	}
	info := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "info.x", Message: "x"})
	if info.Refused == nil || info.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("info.x = %+v", info)
	}
}
