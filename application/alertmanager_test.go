package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// alertmanagerBot is an enabled bot with an alertmanager trigger named
// alerts, and no incident trigger: the alertmanager trigger is consent
// enough.
func alertmanagerBot(t *testing.T, h *harness, template string) domain.Bot {
	t.Helper()
	ctx := context.Background()
	definition := sampleDefinition()
	definition.Name = "triage"
	definition.Triggers = []domain.TriggerSnapshot{{Type: "alertmanager", Name: "alerts", Secret: "s3cret-s3cret-s3cret", Objective: template}}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return bot
}

func alertmanagerBody(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "domain", "testdata", "alertmanager-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func fireAlerts(t *testing.T, h *harness, bot domain.Bot, body []byte) FireTriggerResult {
	t.Helper()
	result, err := NewFireTrigger(h.deps).Execute(context.Background(), FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "alerts", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAlertmanagerGroupIsOneIncidentWhateverItResends(t *testing.T) {
	h := newHarness()
	bot := alertmanagerBot(t, h, "")
	ctx := context.Background()
	first := fireAlerts(t, h, bot, alertmanagerBody(t, "firing"))
	if first.Action != IncidentOpened || !first.Created || first.Task == nil || first.Incident == nil || first.Incident.Status() != domain.IncidentOpen {
		t.Fatalf("first = %+v", first)
	}
	incident := *first.Incident
	if incident.Source() != AlertmanagerSource || incident.Title() != "KubePodCrashLooping in shop" || incident.Severity() != domain.Sev3 || len(incident.Fingerprints()) != 1 || incident.CurrentTaskID() != first.Task.ID() {
		t.Errorf("incident = %+v", incident.Snapshot())
	}
	task := *first.Task
	var compact bytes.Buffer
	_ = json.Compact(&compact, alertmanagerBody(t, "firing"))
	if task.Trigger() != (domain.TriggerRef{Type: domain.TriggerAlertmanager, Name: "alerts"}) || task.IncidentID() != incident.ID() || !bytes.Equal(task.Input(), compact.Bytes()) {
		t.Errorf("task = %+v", task.Snapshot())
	}
	if objective := task.Objective().String(); !strings.Contains(objective, "KubePodCrashLooping in shop") || !strings.Contains(objective, "9c6b1c1ef1b2d3a4") {
		t.Errorf("objective = %q", objective)
	}
	if got := h.log.eventTypes(); got[len(got)-1] != "bot.trigger.fired" || h.log.events[len(got)-1].Payload["action"] != IncidentOpened || h.log.events[len(got)-1].Payload["incidentId"] != incident.ID().String() {
		t.Errorf("events = %v %+v", got, h.log.events[len(got)-1].Payload)
	}
	// Thirty resends of the group (repeat_interval), a second pod among
	// them: one incident, one task.
	for n := 0; n < 30; n++ {
		h.clock.now = h.clock.now.Add(2 * time.Minute)
		body := alertmanagerBody(t, "firing")
		if n%2 == 1 {
			body = alertmanagerBody(t, "repeat")
		}
		again := fireAlerts(t, h, bot, body)
		if again.Action != IncidentRepeated || again.Created || again.Task != nil || again.Incident.ID() != incident.ID() {
			t.Fatalf("resend %d = %+v", n, again)
		}
	}
	if len(h.tasks.tasks) != 1 || len(h.incs.incidents) != 1 {
		t.Fatalf("tasks %d, incidents %d", len(h.tasks.tasks), len(h.incs.incidents))
	}
	after, _ := h.incs.Get(ctx, incident.ID())
	if after.Repeats() != 30 || len(after.Fingerprints()) != 2 {
		t.Errorf("after the resends = %+v", after.Snapshot())
	}
	if last := h.log.events[len(h.log.events)-1]; last.Type != domain.EventTriggerFired || last.Payload["deduplicated"] != true || last.Payload["action"] != IncidentRepeated {
		t.Errorf("last fire = %+v", last)
	}
	// The group goes green: the incident is resolved, no task starts;
	// a second resolved changes nothing.
	resolved := fireAlerts(t, h, bot, alertmanagerBody(t, "resolved"))
	if resolved.Action != IncidentClosed || resolved.Task != nil || resolved.Incident == nil || resolved.Incident.Status() != domain.IncidentResolved || len(resolved.Incident.ResolvedFingerprints()) != 2 || len(resolved.Incident.FiringFingerprints()) != 0 {
		t.Fatalf("resolved = %+v", resolved)
	}
	if !strings.Contains(resolved.Incident.Reason(), "alerts") || !h.log.has("incident.resolved", first.Task.ID()) {
		t.Errorf("reason %q, events %v", resolved.Incident.Reason(), h.log.eventTypes())
	}
	ignored := fireAlerts(t, h, bot, alertmanagerBody(t, "resolved"))
	if ignored.Action != IncidentIgnored || ignored.Incident != nil || ignored.Task != nil {
		t.Errorf("second resolved = %+v", ignored)
	}
	// It fires again within the window: reopened, a second task.
	h.clock.now = h.clock.now.Add(5 * time.Minute)
	reopened := fireAlerts(t, h, bot, alertmanagerBody(t, "firing"))
	if reopened.Action != IncidentReopened || reopened.Created || reopened.Task == nil || reopened.Incident.ID() != incident.ID() || reopened.Incident.Cycle() != 2 || len(reopened.Incident.ResolvedFingerprints()) != 1 {
		t.Fatalf("reopened = %+v", reopened)
	}
	// A firing group that carries a resolved alert marks it.
	mixed := strings.Replace(string(alertmanagerBody(t, "repeat")), `{"status":"firing","labels":{"alertname":"KubePodCrashLooping","container":"web","namespace":"shop","pod":"web-7d9f8c6b5-fghij"`, `{"status":"resolved","labels":{"alertname":"KubePodCrashLooping","container":"web","namespace":"shop","pod":"web-7d9f8c6b5-fghij"`, 1)
	marked := fireAlerts(t, h, bot, []byte(mixed))
	if marked.Action != IncidentRepeated || len(marked.Incident.ResolvedFingerprints()) != 1 || marked.Incident.ResolvedFingerprints()[0] != "0d4e2f3a5b6c7d8e" {
		t.Errorf("mixed = %+v", marked.Incident.Snapshot())
	}
	// A resolved group nobody had is ignored.
	other := strings.Replace(string(alertmanagerBody(t, "resolved")), `namespace=\"shop\"}"`, `namespace=\"other\"}"`, 1)
	if unknown := fireAlerts(t, h, bot, []byte(other)); unknown.Action != IncidentIgnored || unknown.Incident != nil {
		t.Errorf("unknown group = %+v", unknown)
	}
}

func TestAlertmanagerFireRefusals(t *testing.T) {
	h := newHarness()
	bot := alertmanagerBot(t, h, "Look at {{.alert.groupKey}}: {{.title}}")
	ctx := context.Background()
	uc := NewFireTrigger(h.deps)
	var verr domain.ValidationError
	for name, body := range map[string]string{"not alertmanager": `{"type":"deploy"}`, "not json": "{", "empty": ""} {
		if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "alerts", Body: []byte(body)}); !errors.As(err, &verr) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(h.incs.incidents) != 0 {
		t.Error("a refused body opened an incident")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Outcome != domain.AuditDenied || last.Action != "trigger.fire" {
		t.Errorf("audit = %+v", last)
	}
	// The trigger's template renders over the incident and the payload.
	opened := fireAlerts(t, h, bot, alertmanagerBody(t, "firing"))
	if opened.Task == nil || !strings.HasPrefix(opened.Task.Objective().String(), "Look at {}/{alertname=\"KubePodCrashLooping\"}") || !strings.HasSuffix(opened.Task.Objective().String(), "KubePodCrashLooping in shop") {
		t.Errorf("objective = %+v", opened.Task)
	}
	// A paused bot refuses a firing group and saves nothing new.
	if _, err := NewPauseBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	var serr domain.StateError
	other := strings.Replace(string(alertmanagerBody(t, "firing")), `namespace=\"shop\"}"`, `namespace=\"other\"}"`, 1)
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "alerts", Body: []byte(other)}); !errors.As(err, &serr) {
		t.Errorf("paused bot: %v", err)
	}
	if len(h.incs.incidents) != 1 {
		t.Error("a paused bot opened an incident")
	}
	// A failing store is reported.
	_, _ = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	h.incs.listFail = errBoom
	if _, err := uc.Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "alerts", Body: alertmanagerBody(t, "resolved")}); !errors.Is(err, errBoom) {
		t.Errorf("store failure on resolved: %v", err)
	}
}
