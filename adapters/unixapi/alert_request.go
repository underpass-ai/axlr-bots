package unixapi

import "time"

// AlertRequest says a systemd unit failed on this host.
type AlertRequest struct {
	BotID         string    `json:"botId"`
	Unit          string    `json:"unit"`
	Host          string    `json:"host,omitempty"`
	At            time.Time `json:"at,omitempty"`
	WindowSeconds int       `json:"windowSeconds,omitempty"`
	UserManager   bool      `json:"userManager,omitempty"`
	CorrelationID string    `json:"correlationId,omitempty"`
}
