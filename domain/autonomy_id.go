package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// AutonomyID identifies one runbook in one namespace, on one bot's
// ladder: minted from the four, so finding a runbook's history is a Get
// and the same unit is always the same record.
type AutonomyID string

// autonomyIDPrefix starts every id, as inc- does an incident's.
const autonomyIDPrefix = "ladder-"

// AutonomyIDOf is the id of the runbook in the namespace, for the bot of
// the tenant.
func AutonomyIDOf(tenant TenantID, bot BotID, runbook RunbookName, namespace string) AutonomyID {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenant.String(), bot.String(), runbook.String(), namespace}, "\x00")))
	return AutonomyID(autonomyIDPrefix + hex.EncodeToString(sum[:16]))
}

// NewAutonomyID validates an id: what AutonomyIDOf mints.
func NewAutonomyID(raw string) (AutonomyID, error) {
	value, err := validateIdentifier("autonomy id", raw)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(value, autonomyIDPrefix) {
		return "", invalid("autonomy id", "must start with "+autonomyIDPrefix)
	}
	return AutonomyID(value), nil
}

func (id AutonomyID) String() string { return string(id) }
