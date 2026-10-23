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
	if _, err := rand.Read(raw[:]); err != nil {
		panic("system: random source unavailable: " + err.Error())
	}
	return prefix + hex.EncodeToString(raw[:])
}
