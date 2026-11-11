package domainevent

import (
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ToDTO writes the event for the wire, with the trace it belongs to and
// how many team boundaries it crosses with this publish.
func ToDTO(e domain.DomainEvent, tenant domain.TenantID, traceID string, crossings int) DTO {
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
		Data:        DataDTO{ID: e.ID().String(), Context: e.Context().String()},
	}
}

// FromDTO reads an event off the wire: the domain event, and the origin
// a task it makes keeps. It refuses another version, an id that is not
// the event's identity (a forged or changed event) and a crossing count
// past the limit.
func FromDTO(d DTO) (domain.DomainEvent, domain.EventOrigin, error) {
	if d.SpecVersion != SpecVersion {
		return domain.DomainEvent{}, domain.EventOrigin{}, fmt.Errorf("domain event: specVersion %q, want %q", d.SpecVersion, SpecVersion)
	}
	name, err := domain.NewEventName(d.Name)
	if err != nil {
		return domain.DomainEvent{}, domain.EventOrigin{}, err
	}
	id, err := domain.NewDomainID(d.Data.ID)
	if err != nil {
		return domain.DomainEvent{}, domain.EventOrigin{}, err
	}
	var context domain.ContextKey
	if d.Data.Context != "" {
		if context, err = domain.NewContextKey(d.Data.Context); err != nil {
			return domain.DomainEvent{}, domain.EventOrigin{}, err
		}
	}
	at, err := time.Parse(time.RFC3339Nano, d.OccurredAt)
	if err != nil {
		return domain.DomainEvent{}, domain.EventOrigin{}, fmt.Errorf("domain event: occurredAt: %w", err)
	}
	e, err := domain.NewDomainEvent(name, id, context, domain.TeamName(d.Team), domain.BotID(d.From), at)
	if err != nil {
		return domain.DomainEvent{}, domain.EventOrigin{}, err
	}
	if d.ID != e.Identity() {
		return domain.DomainEvent{}, domain.EventOrigin{}, fmt.Errorf("domain event: id %q is not the identity of %s %s %s", d.ID, d.Team, d.Name, d.Data.ID)
	}
	origin := domain.EventOrigin{EventID: d.ID, From: d.From, Team: d.Team, TraceID: d.TraceID, Crossings: d.Crossings, Context: d.Data.Context}
	if err := origin.Validate(); err != nil {
		return domain.DomainEvent{}, domain.EventOrigin{}, err
	}
	return e, origin, nil
}
