package unixapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Headers of the operator API.
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

// bearer requires `Authorization: Bearer <token>` on every request but
// the health check and the trigger routes, which authenticate with the
// trigger's own credential. A node certificate opens nothing here: the
// operator address trusts the token alone.
func bearer(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || (r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/triggers/")) {
			next.ServeHTTP(w, r)
			return
		}
		presented, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="axlr-bots"`)
			respond(w, 0, nil, UnauthorizedError{})
			return
		}
		next.ServeHTTP(w, r)
	})
}
