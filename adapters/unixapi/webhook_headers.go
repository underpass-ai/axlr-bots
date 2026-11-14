package unixapi

// Headers of the webhook routes.
const (
	// IdempotencyKeyHeader carries the key of a POST that creates a task:
	// the same rule as the command's --key. On the trigger route it is
	// the delivery id.
	IdempotencyKeyHeader = "Idempotency-Key"
	// WebhookSecretHeader carries a webhook trigger's shared secret.
	WebhookSecretHeader = "X-Axlr-Secret"
	// WebhookSignatureHeader carries the HMAC-SHA256 of the body under
	// the trigger's signing key, as `sha256=<hex>`; X-Hub-Signature-256
	// (GitHub's) is read too.
	WebhookSignatureHeader = "X-Axlr-Signature"
)
