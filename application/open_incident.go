package application

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DefaultIncidentObjective is what the triage task asks for when neither
// the alert nor the bot's trigger says.
const DefaultIncidentObjective = "Incident {{.id}} ({{.severity}}): {{.title}}. Alerts: {{.fingerprints}}. Find the cause with evidence and report it; change nothing."

// IncidentSource is the source of an incident opened by hand.
const IncidentSource = "manual"

// What an alert did to its incident: opened a new one, repeated the one
// open, reopened one resolved less than the window ago, or resolved it.
const (
	IncidentOpened   = "opened"
	IncidentRepeated = "repeated"
	IncidentReopened = "reopened"
	IncidentClosed   = "resolved"
	IncidentIgnored  = "ignored"
)

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

// opening is one alert group reaching a bot, by hand or through its
// alertmanager trigger: the trigger the triage task comes through (its
// template renders over fields), what the task is given as input, and
// the incident's own facts.
type opening struct {
	bot          domain.Bot
	trigger      domain.Trigger
	source       string
	groupKey     string
	title        string
	severity     domain.IncidentSeverity
	fingerprints []string
	objective    string
	input        []byte
	fields       map[string]any
}

// Execute opens, repeats or reopens the incident by hand: the bot's
// incident trigger is its consent and its template.
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
	trigger, ok := bot.Trigger(domain.TriggerIncident.String())
	if !ok {
		_ = uc.rec.audit(ctx, "incident.open", botID.String(), domain.AuditDenied, map[string]string{"reason": "bot takes no incidents"})
		return OpenIncidentResult{}, domain.StateError{Subject: "bot " + botID.String(), Current: "without an incident trigger", Attempted: "take an incident"}
	}
	return uc.open(ctx, opening{bot: bot, trigger: trigger, source: source, groupKey: command.GroupKey, title: command.Title, severity: severity, fingerprints: command.Fingerprints, objective: command.Objective})
}

// open finds what the group already has and opens, repeats or reopens.
func (uc OpenIncident) open(ctx context.Context, o opening) (OpenIncidentResult, error) {
	bot := o.bot
	if !bot.AcceptsTasks() {
		// Checked before anything is saved; submitTask refuses the same way.
		_ = uc.rec.audit(ctx, "incident.open", bot.ID().String(), domain.AuditDenied, map[string]string{"reason": "bot is " + bot.Status().String()})
		return OpenIncidentResult{}, domain.StateError{Subject: "bot " + bot.ID().String(), Current: bot.Status().String(), Attempted: "take an incident"}
	}
	now := uc.deps.Clock.Now()
	existing, found, err := uc.deps.Incidents.FindNewestByGroupKey(ctx, bot.TenantID(), o.groupKey)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	switch {
	case found && !existing.Status().IsResolved() && len(existing.TaskIDs()) < existing.Cycle():
		// A crash between the incident's save and its task: the alert
		// that comes again gives the cycle its task.
		if err := existing.Repeat(o.fingerprints, now); err != nil {
			return OpenIncidentResult{}, err
		}
		return uc.start(ctx, o, existing, domain.EventIncidentOpened, existing.Cycle() > 1)
	case found && !existing.Status().IsResolved():
		return uc.repeat(ctx, bot, existing, o.fingerprints, now)
	case found && existing.CanReopen(now):
		if err := existing.Reopen(o.fingerprints, now); err != nil {
			return OpenIncidentResult{}, err
		}
		return uc.start(ctx, o, existing, domain.EventIncidentReopened, true)
	}
	id, err := domain.NewIncidentID(uc.deps.IDs.NewID("inc-"))
	if err != nil {
		return OpenIncidentResult{}, err
	}
	incident, err := domain.NewIncident(id, bot.TenantID(), bot.ID(), o.groupKey, o.source, o.title, o.severity, o.fingerprints, now)
	if err != nil {
		return OpenIncidentResult{}, err
	}
	return uc.start(ctx, o, incident, domain.EventIncidentOpened, false)
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
	return OpenIncidentResult{Incident: incident, Action: IncidentRepeated}, nil
}

// start saves the incident (new or reopened) and submits its triage
// task under the key of its cycle; the incident is saved before the
// task, and saved again with the task attached, so a crash between the
// two leaves an incident without a task, found again by its group key.
func (uc OpenIncident) start(ctx context.Context, o opening, incident domain.Incident, kind domain.EventType, reopened bool) (OpenIncidentResult, error) {
	bot := o.bot
	if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
		return OpenIncidentResult{}, err
	}
	snapshot, _ := json.Marshal(incident.Snapshot())
	fields := map[string]any{}
	_ = json.Unmarshal(snapshot, &fields)
	for key, value := range o.fields {
		fields[key] = value
	}
	input := o.input
	if input == nil {
		input = snapshot
	}
	objective := o.objective
	if objective == "" {
		template := o.trigger.ObjectiveTemplate()
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
	result, err := submitTask(ctx, uc.deps, submission{action: "incident.open", bot: bot, trigger: o.trigger.Ref(), objective: parsed, key: key, input: input, incident: incident.ID()})
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
		"fingerprints": incident.Fingerprints(), "cycle": incident.Cycle(), "taskId": result.Task.ID().String(), "trigger": o.trigger.Name(),
	}
	if err := uc.rec.incidentEvent(ctx, kind, bot, incident, payload); err != nil {
		return OpenIncidentResult{}, err
	}
	details := map[string]string{"groupKey": incident.GroupKey(), "task": result.Task.ID().String(), "cycle": strconv.Itoa(incident.Cycle())}
	if err := uc.rec.audit(ctx, "incident.open", incident.ID().String(), domain.AuditOK, details); err != nil {
		return OpenIncidentResult{}, err
	}
	task := result.Task
	action := IncidentOpened
	if reopened {
		action = IncidentReopened
	}
	return OpenIncidentResult{Incident: incident, Task: &task, Created: !reopened, Reopened: reopened, Action: action}, nil
}
