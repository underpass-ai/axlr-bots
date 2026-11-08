package application

import (
	"context"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ClearAutonomy is a person ending a runbook's outstanding applies after
// looking at them: nothing is lowered and nothing earned is lost.
type ClearAutonomy struct {
	deps Dependencies
	rec  recorder
}

// NewClearAutonomy wires the use case.
func NewClearAutonomy(deps Dependencies) ClearAutonomy {
	return ClearAutonomy{deps: deps, rec: recorder{deps: deps}}
}

// Execute clears the entries and answers the runbook's standing. A record
// Get cannot read is the error it gives: nothing is changed.
func (uc ClearAutonomy) Execute(ctx context.Context, id domain.AutonomyID, actor, reason string) (AutonomyView, error) {
	autonomy, err := uc.deps.Autonomy.Get(ctx, id)
	if err != nil {
		return AutonomyView{}, err
	}
	name, err := autonomyActor(ctx, uc.deps, autonomy, actor)
	if err != nil {
		return AutonomyView{}, refuseAutonomy(ctx, uc.rec, "autonomy.clear", id, actor, err)
	}
	cleared, err := autonomy.Clear(name, reason, uc.deps.Clock.Now())
	if err != nil {
		return AutonomyView{}, refuseAutonomy(ctx, uc.rec, "autonomy.clear", id, actor, err)
	}
	if cleared > 0 {
		if err := uc.deps.Autonomy.Save(ctx, autonomy); err != nil {
			return AutonomyView{}, err
		}
	}
	if err := uc.rec.audit(ctx, "autonomy.clear", id.String(), domain.AuditOK, map[string]string{"actor": name, "cleared": strconv.Itoa(cleared)}); err != nil {
		return AutonomyView{}, err
	}
	return viewOf(ctx, uc.deps, autonomy)
}
