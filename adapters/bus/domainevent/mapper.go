package domainevent

import (
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ToDTO writes the event for the wire, with the tenant, the trace it
// belongs to, how many team boundaries it crosses with this publish and
// the teams it is published to.
func ToDTO(e domain.DomainEvent, tenant domain.TenantID, traceID string, crossings int, audience domain.Audience) DTO {
	return DTO{
		SpecVersion: SpecVersion,
		ID:          e.Identity(),
		Name:        e.Name().String(),
		Tenant:      tenant.String(),
		Team:        e.Team().String(),
		From:        e.From().String(),
		OccurredAt:  e.At().Format(time.RFC3339Nano),
		TraceID:     traceID,
		Crossings:   crossings,
		Audience:    audience.Strings(),
		Data:        DataDTO{ID: e.ID().String(), Context: e.Context().String()},
	}
}

// FromDTO reads an event off the wire: the domain event, its tenant, the
// origin a task it makes keeps and its audience. It refuses another
// version, a tenant that is none, an id that is not the event's identity
// (a forged or changed event), a crossing count past the limit and an
// audience that is empty, repeats a team or names the emitting team.
func FromDTO(d DTO) (domain.DomainEvent, domain.TenantID, domain.EventOrigin, domain.Audience, error) {
	fail := func(err error) (domain.DomainEvent, domain.TenantID, domain.EventOrigin, domain.Audience, error) {
		return domain.DomainEvent{}, "", domain.EventOrigin{}, domain.Audience{}, err
	}
	if d.SpecVersion != SpecVersion {
		return fail(fmt.Errorf("domain event: specVersion %q, want %q", d.SpecVersion, SpecVersion))
	}
	tenant, err := domain.NewTenantID(d.Tenant)
	if err != nil {
		return fail(err)
	}
	name, err := domain.NewEventName(d.Name)
	if err != nil {
		return fail(err)
	}
	id, err := domain.NewDomainID(d.Data.ID)
	if err != nil {
		return fail(err)
	}
	var context domain.ContextKey
	if d.Data.Context != "" {
		if context, err = domain.NewContextKey(d.Data.Context); err != nil {
			return fail(err)
		}
	}
	at, err := time.Parse(time.RFC3339Nano, d.OccurredAt)
	if err != nil {
		return fail(fmt.Errorf("domain event: occurredAt: %w", err))
	}
	e, err := domain.NewDomainEvent(name, id, context, domain.TeamName(d.Team), domain.BotID(d.From), at)
	if err != nil {
		return fail(err)
	}
	if d.ID != e.Identity() {
		return fail(fmt.Errorf("domain event: id %q is not the identity of %s %s %s", d.ID, d.Team, d.Name, d.Data.ID))
	}
	origin := domain.EventOrigin{EventID: d.ID, From: d.From, Team: d.Team, TraceID: d.TraceID, Crossings: d.Crossings, Context: d.Data.Context}
	if err := origin.Validate(); err != nil {
		return fail(err)
	}
	audience, err := domain.ParseAudience(e.Team(), d.Audience)
	if err != nil {
		return fail(err)
	}
	return e, tenant, origin, audience, nil
}
