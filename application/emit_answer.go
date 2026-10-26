package application

import "github.com/underpass-ai/axlr-bots/domain"

// EmitAnswer is what the control plane says to an emit: the event's id,
// the teammates that got a task and the ones skipped, or why the event
// was refused as a whole. Deduplicated says the same call had been
// taken before and nothing new was recorded.
type EmitAnswer struct {
	EventID      string
	Name         string
	Severity     domain.Severity
	Deliveries   []EventDelivery
	Skipped      []EventSkip
	Deduplicated bool
	Refused      *domain.EventRefusal
}
