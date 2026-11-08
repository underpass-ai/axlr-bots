package application

import (
	"context"
	"errors"
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
// person answers as a policy or as the daemon's own actor; and the
// approvers of the bot that wrote it apply.
type ReviewPostmortem struct {
	deps Dependencies
	rec  recorder
}

// NewReviewPostmortem wires the use case.
func NewReviewPostmortem(deps Dependencies) ReviewPostmortem {
	return ReviewPostmortem{deps: deps, rec: recorder{deps: deps}}
}

// Approve approves the postmortem's text. A refusal changes nothing.
func (uc ReviewPostmortem) Approve(ctx context.Context, id domain.IncidentID, actor string) (domain.Incident, error) {
	as, name, incident, err := uc.begin(ctx, id, DecisionApprove, actor)
	if err != nil {
		return domain.Incident{}, err
	}
	if err := incident.ApprovePostmortem(name, as.deps.Clock.Now()); err != nil {
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
func (uc ReviewPostmortem) SendBack(ctx context.Context, id domain.IncidentID, actor, note string) (domain.Incident, error) {
	as, name, incident, err := uc.begin(ctx, id, DecisionSendBack, actor)
	if err != nil {
		return domain.Incident{}, err
	}
	if err := incident.SendBackPostmortem(name, strings.TrimSpace(note), as.deps.Clock.Now()); err != nil {
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
	if strings.HasPrefix(name, domain.PolicyActorPrefix) {
		return uc, "", domain.Incident{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is a policy's name: a person answers under their own"})
	}
	if uc.deps.Daemon && domain.ActorKey(name) == domain.ActorKey(uc.deps.Actor) {
		return uc, "", domain.Incident{}, uc.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is the control plane's own name: a person answers under their own"})
	}
	deps := uc.deps
	deps.Actor = name
	as := NewReviewPostmortem(deps)
	incident, err := as.deps.Incidents.Get(ctx, id)
	if err != nil {
		return as, name, domain.Incident{}, err
	}
	postmortem, ok := incident.Postmortem()
	if !ok {
		return as, name, domain.Incident{}, as.refuse(ctx, id, decision, name, domain.StateError{Subject: "incident " + id.String(), Current: "without a postmortem", Attempted: "review it"})
	}
	bot, err := as.deps.Bots.Get(ctx, postmortem.BotID())
	if domain.IsNotFound(err) {
		// Nobody can say who its approvers were: fail closed.
		return as, name, domain.Incident{}, as.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: "bot " + postmortem.BotID().String() + " that wrote it is gone: its approvers cannot be read"})
	}
	if err != nil {
		return as, name, domain.Incident{}, err
	}
	if !bot.Approvals().MayDecide(name) {
		return as, name, domain.Incident{}, as.refuse(ctx, id, decision, name, domain.ForbiddenError{Reason: name + " is not among the approvers of bot " + bot.ID().String()})
	}
	return as, name, incident, nil
}

// refuse audits an answer that was refused, with who gave it, and
// returns its cause.
func (uc ReviewPostmortem) refuse(ctx context.Context, id domain.IncidentID, decision, actor string, cause error) error {
	_ = uc.rec.audit(ctx, "incident.postmortem.refused", id.String(), domain.AuditDenied, map[string]string{"actor": actor, "decision": decision, "reason": cause.Error()})
	return cause
}
