package application

import "github.com/underpass-ai/axlr-bots/domain"

// OutputDeliveryOutcome is one attempt or skip of a pass of the incident
// outputs: the delivery after it and, when it failed or was skipped, why.
type OutputDeliveryOutcome struct {
	Delivery domain.OutputDelivery
	Reason   string
}
