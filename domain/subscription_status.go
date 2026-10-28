package domain

// SubscriptionStatus says whether a subscription hears: an active one
// does, a pending one (a bot's ask the operator has not approved) does
// not.
type SubscriptionStatus string

const (
	// SubscriptionActive hears; absent in the definition.
	SubscriptionActive SubscriptionStatus = "active"
	// SubscriptionPending was asked by the bot and hears nothing until a
	// person approves it.
	SubscriptionPending SubscriptionStatus = "pending"
)

// ParseSubscriptionStatus validates a status; empty is active.
func ParseSubscriptionStatus(raw string) (SubscriptionStatus, error) {
	switch status := SubscriptionStatus(raw); status {
	case "":
		return SubscriptionActive, nil
	case SubscriptionActive, SubscriptionPending:
		return status, nil
	}
	return "", invalid("subscription status", "must be active or pending")
}

func (s SubscriptionStatus) String() string { return string(s) }
