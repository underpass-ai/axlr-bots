package application

import "github.com/underpass-ai/axlr-bots/domain"

// DeliveryOutcome is one attempt or skip of a pass: the delivery after
// it and, when it failed or was skipped, why.
type DeliveryOutcome struct {
	Delivery domain.Delivery
	Reason   string
}
