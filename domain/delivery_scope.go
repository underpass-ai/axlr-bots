package domain

// DeliveryScope says who gets the work a subscription hears or a timer
// fires: the bot that declares it, or one member of its team picked by
// the control plane (docs/architecture/team-subscriptions.md).
type DeliveryScope string

const (
	// DeliverBot gives the work to the bot that declares it; absent in
	// the definition.
	DeliverBot DeliveryScope = ""
	// DeliverTeam gives it to one member of the bot's memory team: the
	// first free one among those that declare the same.
	DeliverTeam DeliveryScope = "team"
)

// ParseDeliveryScope validates a scope; empty is the bot's own.
func ParseDeliveryScope(raw string) (DeliveryScope, error) {
	switch scope := DeliveryScope(raw); scope {
	case DeliverBot, DeliverTeam:
		return scope, nil
	}
	return "", invalid("deliver", "must be team, or absent")
}

// Team reports whether the work goes to one member of the team.
func (s DeliveryScope) Team() bool { return s == DeliverTeam }

func (s DeliveryScope) String() string { return string(s) }
