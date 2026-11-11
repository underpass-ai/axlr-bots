package domain

import (
	"strings"
	"unicode"
)

// maxContextKeyLength bounds the KMP key a domain event may name.
const maxContextKeyLength = 256

// ContextKey is the KMP key (an about of the emitter's team) where the
// context of a domain event lives: data.context. The listener asks the
// emitting team for it through the team channel; it never opens the
// other team's store (docs/architecture/team-topics.md).
type ContextKey string

// NewContextKey validates a key: 1 to 256 printable characters with no
// space, of the form <kind>:<name>.
func NewContextKey(raw string) (ContextKey, error) {
	value := strings.TrimSpace(raw)
	kind, name, ok := strings.Cut(value, ":")
	if !ok || kind == "" || name == "" || len(value) > maxContextKeyLength {
		return "", invalid("data.context", "must be a KMP key <kind>:<name> of at most 256 characters")
	}
	for _, r := range value {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return "", invalid("data.context", "a KMP key has no spaces or control characters")
		}
	}
	return ContextKey(value), nil
}

func (k ContextKey) String() string { return string(k) }
