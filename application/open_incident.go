package application

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DefaultIncidentObjective is what the triage task asks for when neither
// the alert nor the bot's incident trigger says.
const DefaultIncidentObjective = "Incident {{.id}} ({{.severity}}): {{.title}}. Alerts: {{.fingerprints}}. Find the cause with evidence and report it; change nothing."

// IncidentSource is the source of an incident opened by hand.
const IncidentSource = "manual"

// OpenIncident takes an alert group: it opens an incident and the triage
// task of the bot, or records a repeat on the incident the group already
// has, or reopens one resolved less than the window ago. One task per
// cycle: a redelivered alert starts nothing twice.
type OpenIncident struct {
	deps Dependencies
	rec  recorder
}

// NewOpenIncident wires the use case.
func NewOpenIncident(deps Dependencies) OpenIncident {
	return OpenIncident{deps: deps, rec: recorder{deps: deps}}
}

// Execute opens, repeats or reopens the incident.
func (uc OpenIncident) Execute(ctx context.Context, command OpenIncidentCommand) (OpenIncidentResult, error) {
	tenant, err := domain.NewTenantID(command.Tenant)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	botID, err := domain.NewBotID(command.BotID)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	severity, err := domain.ParseIncidentSeverity(command.Severity)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	source := command.Source
	if source == "" {
		source = IncidentSource
	}
	bot, err := uc.deps.Bots.Get(ctx, botID)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	if bot.TenantID() != tenant {
		return OpenIncidentResult{}, domain.NotFoundError{Kind: "bot", ID: botID.String()}
	}
	now := uc.deps.Clock.Now()
	existing, found, err := uc.deps.Incidents.FindNewestByGroupKey(ctx, tenant, command.GroupKey)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	switch {
	case found && !existing.Status().IsResolved() && len(existing.TaskIDs()) < existing.Cycle():
		// A crash between the incident's save and its task: the alert
		// that comes again gives the cycle its task.
		if err := existing.Repeat(command.Fingerprints, now); err != nil {
			return OpenIncidentResult{}, err
		}
		return uc.start(ctx, bot, existing, command.Objective, domain.EventIncidentOpened, existing.Cycle() > 1)
	case found && !existing.Status().IsResolved():
		return uc.repeat(ctx, bot, existing, command.Fingerprints, now)
	case found && existing.CanReopen(now):
		if err := existing.Reopen(command.Fingerprints, now); err != nil {
			return OpenIncidentResult{}, err
		}
		return uc.start(ctx, bot, existing, command.Objective, domain.EventIncidentReopened, true)
	}
	id, err := domain.NewIncidentID(uc.deps.IDs.NewID("inc-"))
	if err != nil {
		return OpenIncidentResult{}, err
	}
	incident, err := domain.NewIncident(id, tenant, botID, command.GroupKey, source, command.Title, severity, command.Fingerprints, now)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	return uc.start(ctx, bot, incident, command.Objective, domain.EventIncidentOpened, false)
}

// repeat records the alert on the open incident and starts nothing.
func (uc OpenIncident) repeat(ctx context.Context, bot domain.Bot, incident domain.Incident, fingerprints []string, now time.Time) (OpenIncidentResult, error) {
	before := len(incident.Fingerprints())
	if err := incident.Repeat(fingerprints, now); err != nil {
		return OpenIncidentResult{}, err
	}
	if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
		return OpenIncidentResult{}, err
	}
	payload := map[string]any{"repeats": incident.Repeats(), "added": incident.Fingerprints()[before:], "status": incident.Status().String()}
	if err := uc.rec.incidentEvent(ctx, domain.EventIncidentRepeated, bot, incident, payload); err != nil {
		return OpenIncidentResult{}, err
	}
	if err := uc.rec.audit(ctx, "incident.open", incident.ID().String(), domain.AuditOK, map[string]string{"groupKey": incident.GroupKey(), "deduplicated": "true"}); err != nil {
		return OpenIncidentResult{}, err
	}
	return OpenIncidentResult{Incident: incident}, nil
}

// start saves the incident (new or reopened) and submits its triage
// task under the key of its cycle; the incident is saved before the
// task, and saved again with the task attached, so a crash between the
// two leaves an incident without a task, found again by its group key.
func (uc OpenIncident) start(ctx context.Context, bot domain.Bot, incident domain.Incident, objective string, kind domain.EventType, reopened bool) (OpenIncidentResult, error) {
	if !bot.AcceptsTrigger(domain.TriggerIncident) {
		_ = uc.rec.audit(ctx, "incident.open", incident.ID().String(), domain.AuditDenied, map[string]string{"reason": "bot takes no incidents"})
		return OpenIncidentResult{}, domain.StateError{Subject: "bot " + bot.ID().String(), Current: "without an incident trigger", Attempted: "take an incident"}
	}
	if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
		return OpenIncidentResult{}, err
	}
	trigger, _ := bot.Trigger(domain.TriggerIncident.String())
	input, _ := json.Marshal(incident.Snapshot())
	var fields map[string]any
	_ = json.Unmarshal(input, &fields)
	if objective == "" {
		template := trigger.ObjectiveTemplate()
		if template == "" {
			template = DefaultIncidentObjective
		}
		rendered, err := domain.RenderObjective(template, fields)
		if err != nil {
			return OpenIncidentResult{}, err
		}
		objective = rendered.String()
	}
	parsed, err := domain.NewObjective(objective)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	key, err := domain.NewIdempotencyKey("incident:" + incident.ID().String() + ":" + strconv.Itoa(incident.Cycle()))
	if err != nil {
		return OpenIncidentResult{}, err
	}
	result, err := submitTask(ctx, uc.deps, submission{action: "incident.open", bot: bot, trigger: trigger.Ref(), objective: parsed, key: key, input: input, incident: incident.ID()})
	if err != nil {
		return OpenIncidentResult{}, err
	}
	if incident.CurrentTaskID() != result.Task.ID() {
		if err := incident.AttachTask(result.Task.ID(), uc.deps.Clock.Now()); err != nil {
			return OpenIncidentResult{}, err
		}
		if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
			return OpenIncidentResult{}, err
		}
	}
	payload := map[string]any{
		"groupKey": incident.GroupKey(), "source": incident.Source(), "title": incident.Title(), "severity": incident.Severity().String(),
		"fingerprints": incident.Fingerprints(), "cycle": incident.Cycle(), "taskId": result.Task.ID().String(),
	}
	if err := uc.rec.incidentEvent(ctx, kind, bot, incident, payload); err != nil {
		return OpenIncidentResult{}, err
	}
	details := map[string]string{"groupKey": incident.GroupKey(), "task": result.Task.ID().String(), "cycle": strconv.Itoa(incident.Cycle())}
	if err := uc.rec.audit(ctx, "incident.open", incident.ID().String(), domain.AuditOK, details); err != nil {
		return OpenIncidentResult{}, err
	}
	task := result.Task
	return OpenIncidentResult{Incident: incident, Task: &task, Created: !reopened, Reopened: reopened}, nil
}
