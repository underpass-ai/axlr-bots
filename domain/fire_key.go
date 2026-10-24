package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// FireKeyWebhook is the idempotency key of a webhook delivery: the
// trigger's name and a hash of the delivery id when the sender gave one
// (Idempotency-Key), else of the body. A redelivery gives the same key
// and so the same task.
func FireKeyWebhook(trigger, deliveryID string, body []byte) IdempotencyKey {
	subject := body
	if deliveryID != "" {
		subject = []byte("delivery:" + deliveryID)
	}
	sum := sha256.Sum256(subject)
	return IdempotencyKey("webhook:" + trigger + ":" + hex.EncodeToString(sum[:16]))
}

// FireKeyTimer is the idempotency key of a timer's slot: the trigger's
// name and the scheduled instant. A scheduler that restarts and fires
// the same slot again finds the same task.
func FireKeyTimer(trigger string, scheduledAt time.Time) IdempotencyKey {
	return IdempotencyKey("timer:" + trigger + ":" + strconv.FormatInt(scheduledAt.Unix(), 10))
}
