package domain

import "strings"

// maxActorLength bounds the name a decision is taken under.
const maxActorLength = 128

// ValidateActor checks the name a person decides under, as the token
// holder asserts it (the CLI's --actor, the panel's operator.actor, the
// X-Axlr-Actor header): not empty, at most 128 characters, no spaces
// and no control characters. Names are compared exactly: cli:tirso is
// not tirso.
func ValidateActor(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid(field, "must not be empty")
	}
	if len(value) > maxActorLength {
		return "", invalid(field, "must be at most 128 characters")
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f {
			return "", invalid(field, "must not contain spaces or control characters")
		}
	}
	return value, nil
}
