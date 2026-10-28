package nodeproto

import "github.com/underpass-ai/axlr-bots/application"

// SubscribeMessage is what a node sends when its execution's bot asks
// to hear a topic of its teams.
type SubscribeMessage struct {
	Topic string `json:"topic"`
	Why   string `json:"why"`
}

// SubscribedMessage is the control plane's answer to an ask: the topic,
// pending, or why it was refused.
type SubscribedMessage struct {
	Topic   string                        `json:"topic"`
	Status  string                        `json:"status,omitempty"`
	Refused *application.SubscribeRefusal `json:"refused,omitempty"`
}

// FromSubscribed renders an ask's answer as a message.
func FromSubscribed(a application.SubscribeAnswer) SubscribedMessage {
	return SubscribedMessage{Topic: a.Topic, Status: a.Status, Refused: a.Refused}
}

// ToSubscribed reads an ask's answer.
func ToSubscribed(m SubscribedMessage) application.SubscribeAnswer {
	return application.SubscribeAnswer{Topic: m.Topic, Status: m.Status, Refused: m.Refused}
}
