package unixapi

import "encoding/json"

// FireRequest fires a bot's webhook trigger (POST /v1/triggers/{bot}/{trigger}):
// the body is the event, a JSON object, and the delivery id is the
// Idempotency-Key header the sender set, if any.
type FireRequest struct {
	BotID      string          `json:"botId"`
	Trigger    string          `json:"trigger"`
	DeliveryID string          `json:"deliveryId,omitempty"`
	Body       json.RawMessage `json:"body"`
}
