package application

// BotReference is another bot that still names a deleted bot: in its
// delegation targets (How "delegation") or as a sender of a topic it
// listens to (How "subscription", with the topic). Nothing breaks: a
// delegation to a gone bot is refused as target_unavailable and a
// subscription simply hears nothing from it; an operator may clean them.
type BotReference struct {
	BotID string `json:"botId"`
	How   string `json:"how"`
	Topic string `json:"topic,omitempty"`
}

// The ways a bot names another.
const (
	ReferenceDelegation   = "delegation"
	ReferenceSubscription = "subscription"
)
