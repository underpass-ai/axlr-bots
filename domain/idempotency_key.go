package domain

import "strings"

// IdempotencyKey makes a task submission repeatable: within one bot, two
// submissions with the same key are the same task, so redelivering the
// activation never starts a second execution. The key is scoped per bot.
type IdempotencyKey string

// NewIdempotencyKey validates a key: 1 to 128 printable characters.
func NewIdempotencyKey(raw string) (IdempotencyKey, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid("idempotency key", "must not be empty")
	}
	if len(value) > 128 {
		return "", invalid("idempotency key", "must be at most 128 characters")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", invalid("idempotency key", "must not contain control characters")
		}
	}
	return IdempotencyKey(value), nil
}

func (k IdempotencyKey) String() string { return string(k) }
