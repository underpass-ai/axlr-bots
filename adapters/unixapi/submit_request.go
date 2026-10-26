package unixapi

// SubmitRequest asks for a task on a bot; the key makes it repeatable.
type SubmitRequest struct {
	BotID          string `json:"botId"`
	Objective      string `json:"objective"`
	IdempotencyKey string `json:"idempotencyKey"`
	CorrelationID  string `json:"correlationId,omitempty"`
	Priority       string `json:"priority,omitempty"`
}
