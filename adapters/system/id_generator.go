package system

import (
	"crypto/rand"
	"encoding/hex"
)

// IDGenerator mints identifiers from 12 random bytes: prefix plus 24 hex
// characters, which satisfies the domain's identifier rules.
type IDGenerator struct{}

// NewID returns prefix followed by 24 random hex characters.
func (IDGenerator) NewID(prefix string) string {
	var raw [12]byte
	// Since Go 1.24 crypto/rand.Read never returns an error: when the
	// kernel's source fails it crashes the program instead.
	_, _ = rand.Read(raw[:])
	return prefix + hex.EncodeToString(raw[:])
}
