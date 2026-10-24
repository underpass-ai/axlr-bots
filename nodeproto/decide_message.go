package nodeproto

import "github.com/underpass-ai/underpass-bots/domain"

// DecideMessage is what the node needs to decide a pending call.
type DecideMessage struct {
	BotID          string                   `json:"botId"`
	SessionID      string                   `json:"sessionId"`
	CallID         string                   `json:"callId,omitempty"`
	Decision       string                   `json:"decision"`
	Level          string                   `json:"level"`
	AllowedTools   []string                 `json:"allowedTools"`
	ToolRules      domain.ToolRulesSnapshot `json:"toolRules"`
	TimeoutSeconds int                      `json:"timeoutSeconds"`
}
