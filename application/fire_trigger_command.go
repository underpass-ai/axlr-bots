package application

import "time"

// FireTriggerCommand fires one of a bot's triggers. A webhook fire
// carries the request body (a JSON object, the task's input and the
// objective template's data) and the delivery id the sender gave
// (Idempotency-Key), if any; a timer fire carries the slot it is for.
type FireTriggerCommand struct {
	BotID       string
	TriggerName string
	DeliveryID  string
	Body        []byte
	ScheduledAt time.Time
}
