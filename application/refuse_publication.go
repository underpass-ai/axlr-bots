package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RefusedPublication is a publication an inbound adapter (a broker's
// subject, say) took off for good: the tenant and the team it pointed
// at, as far as it could read them, the event's name, its idempotency
// key and why. Nothing was delivered.
type RefusedPublication struct {
	Tenant string
	Team   string
	Name   string
	Key    string
	Reason string
}

// RefusePublication records a refused publication in the audit log as
// event.publish, denied: the operator's way in that no answer reaches
// still leaves a trace of what it turned away.
type RefusePublication struct {
	rec recorder
}

// NewRefusePublication wires the use case.
func NewRefusePublication(deps Dependencies) RefusePublication {
	return RefusePublication{rec: recorder{deps: deps}}
}

// Execute writes the audit entry.
func (uc RefusePublication) Execute(ctx context.Context, p RefusedPublication) error {
	subject := p.Team
	if subject == "" {
		subject = "unknown"
	}
	details := map[string]string{"reason": p.Reason}
	for key, value := range map[string]string{"tenant": p.Tenant, "group": p.Team, "name": p.Name, "key": p.Key} {
		if value != "" {
			details[key] = value
		}
	}
	return uc.rec.audit(ctx, "event.publish", subject, domain.AuditDenied, details)
}
