package application

// SubscribeToolName is the tool the node serves to an execution whose
// bot is in a team, next to the emit tool: the bot asks its operator to
// let it hear a topic.
const SubscribeToolName = "axlr_bots_subscribe"

// SubscribeCommand is what an execution's call of the subscribe tool
// asks: a name or pattern of its teams and why it would hear it.
type SubscribeCommand struct {
	Topic string `json:"topic"`
	Why   string `json:"why"`
}

// SubscribeAnswer is what the control plane says to an ask: the topic,
// pending until a person decides, or why it was refused.
type SubscribeAnswer struct {
	Topic   string            `json:"topic"`
	Status  string            `json:"status,omitempty"`
	Refused *SubscribeRefusal `json:"refused,omitempty"`
}

// SubscribeRefusal says why an ask was refused, in words the model can
// act on.
type SubscribeRefusal struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
