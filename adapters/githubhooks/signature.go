package githubhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// signaturePrefix starts X-Hub-Signature-256: sha256=<hex>.
const signaturePrefix = "sha256="

// Sign is X-Hub-Signature-256 of body with secret, as GitHub computes it.
func Sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether header is the signature of body with secret,
// compared in constant time. A missing or malformed header is false.
func Verify(secret, body []byte, header string) bool {
	given, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok {
		return false
	}
	sum, err := hex.DecodeString(given)
	if err != nil || len(sum) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(sum, mac.Sum(nil))
}
