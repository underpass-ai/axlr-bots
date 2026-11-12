package application

import "github.com/underpass-ai/axlr-bots/domain"

// CrossingEvent is what the domain-event bus carries from one team to
// others (docs/architecture/team-topics.md): the tenant both teams are
// in, the domain event, the teams it is published to, the trace it
// belongs to and how many team boundaries the trace crossed with this
// publish. The wire form is an adapter's; this is the value the use
// cases hand over and take back.
type CrossingEvent struct {
	Tenant    domain.TenantID
	Event     domain.DomainEvent
	Audience  domain.Audience
	TraceID   string
	Crossings int
}

// Origin is what a task the event makes keeps of where it came from.
func (c CrossingEvent) Origin() domain.EventOrigin {
	return domain.EventOrigin{
		EventID: c.Event.Identity(), From: c.Event.From().String(), Team: c.Event.Team().String(),
		TraceID: c.TraceID, Crossings: c.Crossings, Context: c.Event.Context().String(),
	}
}
