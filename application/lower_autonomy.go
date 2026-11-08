package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// LowerAutonomy is a person stopping a runbook: one step down by hand,
// whatever it earned, and every outstanding apply ended. A task already
// granted is taken back when it comes to run. The actor must be named, a
// person and one of the apply bot's approvers.
type LowerAutonomy struct {
	deps Dependencies
	rec  recorder
}

// NewLowerAutonomy wires the use case.
func NewLowerAutonomy(deps Dependencies) LowerAutonomy {
	return LowerAutonomy{deps: deps, rec: recorder{deps: deps}}
}

// Execute lowers the runbook and answers its standing.
func (uc LowerAutonomy) Execute(ctx context.Context, id domain.AutonomyID, actor, reason string) (AutonomyView, error) {
	autonomy, err := uc.deps.Autonomy.Get(ctx, id)
	if err != nil {
		return AutonomyView{}, err
	}
	name, err := domain.ValidateActor("actor", actor)
	if err != nil {
		return AutonomyView{}, uc.refuse(ctx, id, actor, domain.ForbiddenError{Reason: "a runbook is lowered by a named person: name who lowers it (--actor, X-Axlr-Actor or loweredBy)"})
	}
	if !personName(ctx, uc.deps, name) {
		return AutonomyView{}, uc.refuse(ctx, id, actor, domain.ForbiddenError{Reason: name + " is not a person's name"})
	}
	bot, err := uc.deps.Bots.Get(ctx, autonomy.BotID())
	if err != nil && !domain.IsNotFound(err) {
		return AutonomyView{}, err
	}
	if err == nil && !bot.Approvals().MayDecide(name) {
		return AutonomyView{}, uc.refuse(ctx, id, actor, domain.ForbiddenError{Reason: name + " is not among the approvers of bot " + bot.ID().String()})
	}
	from := autonomy.Step()
	if err := autonomy.Lower(name, reason, uc.deps.Clock.Now()); err != nil {
		return AutonomyView{}, uc.refuse(ctx, id, actor, err)
	}
	if err := uc.deps.Autonomy.Save(ctx, autonomy); err != nil {
		return AutonomyView{}, err
	}
	details := map[string]string{"actor": name, "from": from.String(), "to": autonomy.Step().String(), "signal": domain.SignalLoweredByPerson.String()}
	if reason != "" {
		details["reason"] = reason
	}
	if err := uc.rec.audit(ctx, "autonomy.changed", id.String(), domain.AuditOK, details); err != nil {
		return AutonomyView{}, err
	}
	return viewOf(ctx, uc.deps, autonomy)
}

func (uc LowerAutonomy) refuse(ctx context.Context, id domain.AutonomyID, actor string, cause error) error {
	_ = uc.rec.audit(ctx, "autonomy.lower", id.String(), domain.AuditDenied, map[string]string{"actor": actor, "reason": cause.Error()})
	return cause
}
