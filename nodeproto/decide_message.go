package nodeproto

import "github.com/underpass-ai/axlr-bots/domain"

// DecideMessage is what the node needs to decide a pending call; the
// task and the trace are the correlation the runtime receives.
type DecideMessage struct {
	TaskID         string                   `json:"taskId,omitempty"`
	BotID          string                   `json:"botId"`
	CorrelationID  string                   `json:"correlationId,omitempty"`
	SessionID      string                   `json:"sessionId"`
	CallID         string                   `json:"callId,omitempty"`
	Decision       string                   `json:"decision"`
	Level          string                   `json:"level"`
	AllowedTools   []string                 `json:"allowedTools"`
	ToolRules      domain.ToolRulesSnapshot `json:"toolRules"`
	TimeoutSeconds int                      `json:"timeoutSeconds"`
	Delegation     *DelegationOfferMessage  `json:"delegation,omitempty"`
	Events         *EventsOfferMessage      `json:"events,omitempty"`
}
