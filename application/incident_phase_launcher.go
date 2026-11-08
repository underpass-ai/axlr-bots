package application

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Default objectives of the phases after triage, used when the taking
// bot's incident trigger does not say. Verify's asks for the verdict line
// domain.ParseVerifyVerdict reads.
const (
	DefaultFixObjective    = "Incident {{.id}} ({{.severity}}): {{.title}}. Alerts: {{.fingerprints}}. Triage diagnosed it:\n\n{{.diagnosis}}\n\nPropose the fix and its rollback; apply nothing."
	DefaultApplyObjective  = "Incident {{.id}} ({{.severity}}): {{.title}}. Triage diagnosed it:\n\n{{.diagnosis}}\n\nThe fix phase proposed:\n\n{{.proposal}}\n\nApply the proposal as written, one change, and report what you applied and how to roll it back."
	DefaultVerifyObjective = "Incident {{.id}} ({{.severity}}): {{.title}}. Alerts: {{.fingerprints}}. This change was applied:\n\n{{.proposal}}\n\nCheck that it took and that the fault is gone; change nothing. Write the line VERDICT: green when it took, or VERDICT: red when the fault is still there."
)

// minPhaseObjectiveText is the least of the diagnosis or the proposal a
// phase's objective carries, however long its template.
const minPhaseObjectiveText = 16

// phaseLauncher launches the phase an incident has reached after triage:
// fix once it is Diagnosed, apply once it is Proposed, verify once it is
// Verifying. The teammate of its triage bot that declares an incident
// trigger with that phase gets a task of its own, linked to the
// incident, with the diagnosis and the proposal. Only the control plane
// launches a phase (design §4): a bot that delegated the fix and waited
// for it would wait through an approval.
type phaseLauncher struct {
	deps Dependencies
	rec  recorder
}

func newPhaseLauncher(deps Dependencies) phaseLauncher {
	return phaseLauncher{deps: deps, rec: recorder{deps: deps}}
}

// duePhase is the phase an incident's status launches, and false for a
// status that launches none from here (triage starts on an alert).
func duePhase(status domain.IncidentStatus) (domain.IncidentPhase, bool) {
	for _, phase := range []domain.IncidentPhase{domain.IncidentPhaseFix, domain.IncidentPhaseApply, domain.IncidentPhaseVerify} {
		if phase.LaunchedAt() == status {
			return phase, true
		}
	}
	return "", false
}

// launch submits the task of the phase the incident has reached, in its
// current cycle and under the key of its cycle and phase, and attaches
// it. An incident that has the task already, or whose status launches no
// phase, is left alone. With no teammate taking the phase it stays where
// it is (Diagnosed, Proposed or Verifying), as before the phase existed;
// with two the choice is not the control plane's, and a bot that refuses
// the task (paused, a template that does not render) leaves it there
// too: both are audited, and the next alert of the group tries again.
// The task carries the previous phase's trace on: the correlation given,
// or the current task's own when none is. It returns the incident as it
// saved it.
func (l phaseLauncher) launch(ctx context.Context, incident domain.Incident, correlation domain.CorrelationID) (domain.Incident, error) {
	phase, due := duePhase(incident.Status())
	if !due || incident.HasTask(phase) {
		return incident, nil
	}
	triage, err := l.deps.Bots.Get(ctx, incident.BotID())
	if err != nil {
		if domain.IsNotFound(err) {
			return incident, nil
		}
		return incident, err
	}
	mates, err := teammatesOf(ctx, l.deps, triage)
	if err != nil {
		return incident, err
	}
	var (
		taker   domain.Bot
		trigger domain.Trigger
		takers  []string
	)
	for _, mate := range mates {
		if found, ok := mate.Triggers().Incident(phase); ok {
			taker, trigger = mate, found
			takers = append(takers, mate.ID().String())
		}
	}
	switch {
	case len(takers) == 0:
		return incident, nil
	case len(takers) > 1:
		return incident, l.refuse(ctx, incident, phase, "teammates "+strings.Join(takers, ", ")+" all take the "+phase.String()+" phase; one must")
	}
	if correlation == "" {
		if current, err := l.deps.Tasks.Get(ctx, incident.CurrentTaskID()); err == nil {
			correlation = current.CorrelationID()
		}
	}
	fields, input := incidentFields(incident)
	objective, err := phaseObjective(phase, trigger, incident, fields)
	if err != nil {
		return incident, l.refuse(ctx, incident, phase, err.Error())
	}
	key, err := domain.NewIdempotencyKey("incident:" + incident.ID().String() + ":" + strconv.Itoa(incident.Cycle()) + ":" + phase.String())
	if err != nil {
		return incident, err
	}
	action := "incident." + phase.String()
	var ladder domain.LadderDecision
	if phase == domain.IncidentPhaseApply {
		ladder = newLadderKeeper(l.deps).decide(ctx, taker, trigger, incident)
	}
	result, err := submitTask(ctx, l.deps, submission{action: action, bot: taker, trigger: trigger.Ref(), objective: objective, key: key, correlation: correlation, input: input, incident: incident.ID(), grant: ladder.Grant})
	if err != nil {
		var state domain.StateError
		if errors.As(err, &state) {
			return incident, l.refuse(ctx, incident, phase, err.Error())
		}
		return incident, err
	}
	launched := incident
	if err := launched.AttachPhaseTask(result.Task.ID(), taker.ID(), phase, l.deps.Clock.Now()); err != nil {
		return incident, err
	}
	if err := l.deps.Incidents.Save(ctx, launched); err != nil {
		return incident, err
	}
	policy := taker.PolicyFor(trigger.Ref())
	details := map[string]string{"task": result.Task.ID().String(), "bot": taker.ID().String(), "cycle": strconv.Itoa(launched.Cycle()), "level": policy.Level().String()}
	if ladder.Laddered {
		// Every apply on a ladder says why it has its card or has none.
		details["ladder"] = ladder.Reason
		if ladder.Grant != nil {
			details["level"] = domain.AutonomyActWithinPolicy.String()
		}
	}
	return launched, l.rec.audit(ctx, action, launched.ID().String(), domain.AuditOK, details)
}

// refuse audits why the phase did not start; the incident stays where
// it is.
func (l phaseLauncher) refuse(ctx context.Context, incident domain.Incident, phase domain.IncidentPhase, reason string) error {
	return l.rec.audit(ctx, "incident."+phase.String(), incident.ID().String(), domain.AuditDenied, map[string]string{"reason": reason, "cycle": strconv.Itoa(incident.Cycle())})
}

// phaseObjective renders the phase trigger's template, or the phase's
// default, over the incident. A diagnosis or a proposal may each be
// longer than an objective: they are cut to the room the template
// leaves (a third for the diagnosis when the template names both), and
// the task's input carries them whole.
func phaseObjective(phase domain.IncidentPhase, trigger domain.Trigger, incident domain.Incident, fields map[string]any) (domain.Objective, error) {
	template := trigger.ObjectiveTemplate()
	if template == "" {
		template = defaultPhaseObjective(phase)
	}
	fields["diagnosis"], fields["proposal"] = "", ""
	base, err := domain.RenderObjective(template, fields)
	if err != nil {
		return "", err
	}
	room := max(domain.MaxObjectiveLength-len(base.String()), minPhaseObjectiveText)
	diagnosisRoom := room
	if strings.Contains(template, ".diagnosis") && strings.Contains(template, ".proposal") {
		diagnosisRoom = max(room/3, minPhaseObjectiveText)
	}
	if incident.Diagnosis() != "" {
		fields["diagnosis"] = domain.IncidentText(incident.Diagnosis(), diagnosisRoom)
	}
	proposalRoom := room
	if diagnosisRoom != room {
		proposalRoom = max(room-len(fields["diagnosis"].(string)), minPhaseObjectiveText)
	}
	if incident.Proposal() != "" {
		fields["proposal"] = domain.IncidentText(incident.Proposal(), proposalRoom)
	}
	return domain.RenderObjective(template, fields)
}

func defaultPhaseObjective(phase domain.IncidentPhase) string {
	switch phase {
	case domain.IncidentPhaseApply:
		return DefaultApplyObjective
	case domain.IncidentPhaseVerify:
		return DefaultVerifyObjective
	}
	return DefaultFixObjective
}
