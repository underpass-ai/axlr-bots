package application

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DecisionSendBack is a person's other answer to a postmortem.
const DecisionSendBack = "send-back"

// ReviewPostmortem is a person's answer to an incident's postmortem:
// approve, which announces it through the incident's outputs, or send it
// back with a note, which launches its task again up to
// domain.IncidentMaxPostmortemSendBacks and then leaves it to a person.
// The actor is asserted as on a task's decision and must be named; no
// person answers under a policy's or a trigger's name, a registered
// bot's id or the daemon's own actor; and the approvers of the bot that
// wrote it apply. Every answer names the task whose text the person read,
// so that nobody approves or sends back a text they never saw. On a
// Resolved incident with no postmortem of its cycle, a send-back asks for
// one: the way out when its first launch could not start.
type ReviewPostmortem struct {
	deps Dependencies
	rec  recorder
}

// NewReviewPostmortem wires the use case.
func NewReviewPostmortem(deps Dependencies) ReviewPostmortem {
	return ReviewPostmortem{deps: deps, rec: recorder{deps: deps}}
}

// Approve approves the postmortem's text. A refusal changes nothing.
func (uc ReviewPostmortem) Approve(ctx context.Context, id domain.IncidentID, actor string, read domain.TaskID) (domain.Incident, error) {
	as, name, incident, err := uc.begin(ctx, id, DecisionApprove, actor)
	if err != nil {
		return domain.Incident{}, err
	}
	if err := incident.ApprovePostmortem(name, read, as.deps.Clock.Now()); err != nil {
		return domain.Incident{}, as.refuse(ctx, id, DecisionApprove, name, err)
	}
	if err := as.deps.Incidents.Save(ctx, incident); err != nil {
		return domain.Incident{}, err
	}
	postmortem, _ := incident.Postmortem()
	details := map[string]string{"actor": name, "authorTask": postmortem.AuthorTaskID().String(), "cycle": strconv.Itoa(postmortem.Cycle())}
	payload := map[string]any{"approvedBy": name, "taskId": postmortem.AuthorTaskID().String(), "cycle": postmortem.Cycle(), "status": incident.Status().String()}
	if err := as.rec.incidentEvent(ctx, domain.EventIncidentPostmortemApproved, incident, payload); err != nil {
		return domain.Incident{}, err
	}
	if err := as.rec.audit(ctx, "incident.postmortem.approve", id.String(), domain.AuditOK, details); err != nil {
		return domain.Incident{}, err
	}
	return incident, nil
}

// SendBack records the person's note and launches the postmortem's task
// again, or leaves the postmortem to a person at the last send-back. A
// launch that does not start is audited: the postmortem stays SentBack
// and a send-back again tries it.
func (uc ReviewPostmortem) SendBack(ctx context.Context, id domain.IncidentID, actor, note string, read domain.TaskID) (domain.Incident, error) {
	as, name, incident, err := uc.begin(ctx, id, DecisionSendBack, actor)
	if err != nil {
		return domain.Incident{}, err
	}
	if err := as.settleEnded(ctx, &incident); err != nil {
		return domain.Incident{}, err
	}
	if err := incident.SendBackPostmortem(name, strings.TrimSpace(note), read, as.deps.Clock.Now()); err != nil {
		var state domain.StateError
		if errors.As(err, &state) {
			return domain.Incident{}, as.refuse(ctx, id, DecisionSendBack, name, err)
		}
		return domain.Incident{}, err
	}
	if err := as.deps.Incidents.Save(ctx, incident); err != nil {
		return domain.Incident{}, err
	}
	postmortem, _ := incident.Postmortem()
	payload := map[string]any{"by": name, "sendBacks": postmortem.SendBacks(), "state": postmortem.State().String(), "cycle": postmortem.Cycle(), "status": incident.Status().String()}
	if err := as.rec.incidentEvent(ctx, domain.EventIncidentPostmortemSentBack, incident, payload); err != nil {
		return domain.Incident{}, err
	}
	details := map[string]string{"actor": name, "state": postmortem.State().String(), "sendBacks": strconv.Itoa(postmortem.SendBacks()), "cycle": strconv.Itoa(postmortem.Cycle())}
	if err := as.rec.audit(ctx, "incident.postmortem.send_back", id.String(), domain.AuditOK, details); err != nil {
		return domain.Incident{}, err
	}
	return newPostmortemLauncher(as.deps).launch(ctx, incident), nil
}

// begin validates who answers and reads the incident; the use case it
// returns records under that name.
func (uc ReviewPostmortem) begin(ctx context.Context, id domain.IncidentID, decision, actor string) (ReviewPostmortem, string, domain.Incident, error) {
	if strings.TrimSpace(actor) == "" {
		return uc, "", domain.Incident{}, uc.refuse(ctx, id, decision, "", domain.ForbiddenError{Reason: "a postmortem's review names who answers (--actor, the panel's operator.actor, X-Axlr-Actor or decidedBy)"})
	}
	name, err := domain.ValidateActor("decidedBy", actor)
	if err != nil {
		return uc, "", domain.Incident{}, err
	}
	if domain.IsMachineActor(name) {
		return uc, "", domain.Incident{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is a policy's or a trigger's name: a person answers under their own"})
	}
	if uc.deps.Daemon && domain.ActorKey(name) == domain.ActorKey(uc.deps.Actor) {
		return uc, "", domain.Incident{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is the control plane's own name: a person answers under their own"})
	}
	bots, err := uc.deps.Bots.List(ctx)
	if err != nil {
		return uc, "", domain.Incident{}, err
	}
	if slices.ContainsFunc(bots, func(bot domain.Bot) bool { return domain.ActorKey(bot.ID().String()) == domain.ActorKey(name) }) {
		return uc, "", domain.Incident{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is a bot's id: a person answers under their own"})
	}
	deps := uc.deps
	deps.Actor = name
	as := NewReviewPostmortem(deps)
	incident, err := as.deps.Incidents.Get(ctx, id)
	if err != nil {
		return as, name, domain.Incident{}, err
	}
	bot, err := as.reviewer(ctx, incident, decision, name)
	if err != nil {
		return as, name, domain.Incident{}, err
	}
	if !bot.Approvals().MayDecide(name) {
		return as, name, domain.Incident{}, as.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is not among the approvers of bot " + bot.ID().String()})
	}
	return as, name, incident, nil
}

// reviewer is the bot whose approvers answer the incident's postmortem:
// the one that took the phase, or, when no task has started for this
// cycle, the one that would (a send-back asks it to start; its severity
// must be one it writes for).
func (uc ReviewPostmortem) reviewer(ctx context.Context, incident domain.Incident, decision, name string) (domain.Bot, error) {
	id := incident.ID()
	postmortem, ok := incident.Postmortem()
	if ok && postmortem.Cycle() == incident.Cycle() && postmortem.BotID() != "" {
		bot, err := uc.deps.Bots.Get(ctx, postmortem.BotID())
		if domain.IsNotFound(err) {
			// Nobody can say who its approvers were: fail closed.
			return domain.Bot{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: "bot " + postmortem.BotID().String() + " that wrote it is gone: its approvers cannot be read"})
		}
		return bot, err
	}
	if decision != DecisionSendBack || !incident.Status().IsResolved() {
		return domain.Bot{}, uc.refuse(ctx, id, decision, name, domain.StateError{Subject: "incident " + id.String(), Current: "without a postmortem of its cycle", Attempted: "review it"})
	}
	bot, trigger, found, err := newPostmortemLauncher(uc.deps).taker(ctx, incident)
	if err != nil {
		return domain.Bot{}, err
	}
	if !found {
		return domain.Bot{}, uc.refuse(ctx, id, decision, name, domain.StateError{Subject: "incident " + id.String(), Current: "without one teammate taking the postmortem phase", Attempted: "ask for its postmortem"})
	}
	if !trigger.WritesPostmortemFor(incident.Severity()) {
		return domain.Bot{}, uc.refuse(ctx, id, decision, name, domain.StateError{Subject: "incident " + id.String(), Current: "of a severity trigger " + trigger.Name() + " does not write one for", Attempted: "ask for its postmortem"})
	}
	return bot, nil
}

// settleEnded closes a postmortem still Writing on a task that is over
// or gone: the task's end never reached it (a store that failed, a
// crash), and without this nothing could ever move it.
func (uc ReviewPostmortem) settleEnded(ctx context.Context, incident *domain.Incident) error {
	postmortem, ok := incident.Postmortem()
	if !ok || postmortem.State() != domain.PostmortemWriting || postmortem.Cycle() != incident.Cycle() {
		return nil
	}
	task, err := uc.deps.Tasks.Get(ctx, postmortem.TaskID())
	if err != nil && !domain.IsNotFound(err) {
		return err
	}
	if err == nil && !task.Status().IsTerminal() {
		return nil
	}
	return incident.FailPostmortem(postmortem.TaskID(), "its task ended without settling it", uc.deps.Clock.Now())
}

// refuse audits an answer that was refused, with who gave it, and
// returns its cause.
func (uc ReviewPostmortem) refuse(ctx context.Context, id domain.IncidentID, decision, actor string, cause error) error {
	_ = uc.rec.audit(ctx, "incident.postmortem.refused", id.String(), domain.AuditDenied, map[string]string{"actor": actor, "decision": decision, "reason": cause.Error()})
	return cause
}
