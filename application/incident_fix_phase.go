package application

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DefaultFixObjective is what the fix task asks for when the fix bot's
// incident trigger does not say.
const DefaultFixObjective = "Incident {{.id}} ({{.severity}}): {{.title}}. Alerts: {{.fingerprints}}. Triage diagnosed it:\n\n{{.diagnosis}}\n\nPropose the fix and its rollback; apply nothing."

// minFixObjectiveDiagnosis is the least of the diagnosis a fix objective
// carries, however long its template.
const minFixObjectiveDiagnosis = 16

// fixPhase launches the fix phase of a diagnosed incident: the teammate
// of its triage bot that declares an incident trigger with phase fix
// gets a task of its own, linked to the incident, with the diagnosis.
// Only the control plane launches a phase (design §4): a bot that
// delegated the fix and waited for it would wait through an approval.
type fixPhase struct {
	deps Dependencies
	rec  recorder
}

func newFixPhase(deps Dependencies) fixPhase {
	return fixPhase{deps: deps, rec: recorder{deps: deps}}
}

// launch submits the fix task of the incident's current cycle, under the
// key of its cycle, and attaches it. An incident that is not Diagnosed,
// or has its fix task already, is left alone. With no teammate taking
// the fix phase it stays Diagnosed, as before there was a fix phase;
// with two the choice is not the control plane's, and a fix bot that
// refuses the task (paused, a template that does not render) leaves it
// Diagnosed too: both are audited, and the next alert of the group
// tries again. The correlation id carries the triage task's trace on.
// It returns the incident as it saved it.
func (f fixPhase) launch(ctx context.Context, incident domain.Incident, correlation domain.CorrelationID) (domain.Incident, error) {
	if incident.Status() != domain.IncidentDiagnosed || incident.HasTask(domain.IncidentPhaseFix) {
		return incident, nil
	}
	triage, err := f.deps.Bots.Get(ctx, incident.BotID())
	if err != nil {
		if domain.IsNotFound(err) {
			return incident, nil
		}
		return incident, err
	}
	mates, err := teammatesOf(ctx, f.deps, triage)
	if err != nil {
		return incident, err
	}
	var (
		fixer   domain.Bot
		trigger domain.Trigger
		takers  []string
	)
	for _, mate := range mates {
		if found, ok := mate.Triggers().Incident(domain.IncidentPhaseFix); ok {
			fixer, trigger = mate, found
			takers = append(takers, mate.ID().String())
		}
	}
	switch {
	case len(takers) == 0:
		return incident, nil
	case len(takers) > 1:
		return incident, f.refuse(ctx, incident, "teammates "+strings.Join(takers, ", ")+" all take the fix phase; one must")
	}
	fields, input := incidentFields(incident)
	objective, err := fixObjective(trigger, incident, fields)
	if err != nil {
		return incident, f.refuse(ctx, incident, err.Error())
	}
	key, err := domain.NewIdempotencyKey("incident:" + incident.ID().String() + ":" + strconv.Itoa(incident.Cycle()) + ":fix")
	if err != nil {
		return incident, err
	}
	result, err := submitTask(ctx, f.deps, submission{action: "incident.fix", bot: fixer, trigger: trigger.Ref(), objective: objective, key: key, correlation: correlation, input: input, incident: incident.ID()})
	if err != nil {
		var state domain.StateError
		if errors.As(err, &state) {
			return incident, f.refuse(ctx, incident, err.Error())
		}
		return incident, err
	}
	launched := incident
	if err := launched.AttachFixTask(result.Task.ID(), fixer.ID(), f.deps.Clock.Now()); err != nil {
		return incident, err
	}
	if err := f.deps.Incidents.Save(ctx, launched); err != nil {
		return incident, err
	}
	details := map[string]string{"task": result.Task.ID().String(), "bot": fixer.ID().String(), "cycle": strconv.Itoa(launched.Cycle())}
	return launched, f.rec.audit(ctx, "incident.fix", launched.ID().String(), domain.AuditOK, details)
}

// refuse audits why the fix phase did not start; the incident stays
// Diagnosed.
func (f fixPhase) refuse(ctx context.Context, incident domain.Incident, reason string) error {
	return f.rec.audit(ctx, "incident.fix", incident.ID().String(), domain.AuditDenied, map[string]string{"reason": reason, "cycle": strconv.Itoa(incident.Cycle())})
}

// fixObjective renders the fix trigger's template, or
// DefaultFixObjective, over the incident. A diagnosis may be twice as
// long as an objective: it is cut to the room the template leaves, and
// the task's input carries it whole.
func fixObjective(trigger domain.Trigger, incident domain.Incident, fields map[string]any) (domain.Objective, error) {
	template := trigger.ObjectiveTemplate()
	if template == "" {
		template = DefaultFixObjective
	}
	fields["diagnosis"] = ""
	base, err := domain.RenderObjective(template, fields)
	if err != nil {
		return "", err
	}
	room := max(domain.MaxObjectiveLength-len(base.String()), minFixObjectiveDiagnosis)
	fields["diagnosis"] = domain.IncidentText(incident.Diagnosis(), room)
	return domain.RenderObjective(template, fields)
}
