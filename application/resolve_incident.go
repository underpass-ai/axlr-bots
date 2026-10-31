package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ResolveIncident closes an incident: the operator says it is over, or
// the source said its alerts went green. The reason is kept on the
// incident and in its event.
type ResolveIncident struct {
	deps Dependencies
	rec  recorder
}

// NewResolveIncident wires the use case.
func NewResolveIncident(deps Dependencies) ResolveIncident {
	return ResolveIncident{deps: deps, rec: recorder{deps: deps}}
}

// Execute resolves the incident; a resolved one is refused with a
// domain.StateError.
func (uc ResolveIncident) Execute(ctx context.Context, id domain.IncidentID, reason string) (domain.Incident, error) {
	incident, err := uc.deps.Incidents.Get(ctx, id)
	if err != nil {
		return domain.Incident{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, incident.BotID())
	if err != nil {
		return domain.Incident{}, err
	}
	if err := incident.Resolve(reason, uc.deps.Clock.Now()); err != nil {
		_ = uc.rec.audit(ctx, "incident.resolve", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Incident{}, err
	}
	if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
		return domain.Incident{}, err
	}
	payload := map[string]any{"by": uc.deps.Actor, "reason": reason, "cycle": incident.Cycle(), "costUsd": incident.CostUSD()}
	if err := uc.rec.incidentEvent(ctx, domain.EventIncidentResolved, bot, incident, payload); err != nil {
		return domain.Incident{}, err
	}
	if err := uc.rec.audit(ctx, "incident.resolve", id.String(), domain.AuditOK, map[string]string{"reason": reason}); err != nil {
		return domain.Incident{}, err
	}
	return incident, nil
}
