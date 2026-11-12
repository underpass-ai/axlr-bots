package domain

import (
	"bytes"
	"encoding/json"
	"time"
)

// maxStagedChangeBytes bounds the change a bot holds staged: the API's
// body limit already does, this keeps a hand-written file honest too.
const maxStagedChangeBytes = 1 << 20

// StagedDefinition is a change to a bot's definition that waits to be
// applied: an installer leaves the next version of the bot on the
// control plane, the operator sees the bot is behind it and applies it
// when they choose. The change is the patch the update takes, kept as
// the JSON it was staged with: the domain stores it, the application
// reads it. Its version is the definition's once applied.
type StagedDefinition struct {
	version  SemanticVersion
	stagedAt time.Time
	change   json.RawMessage
}

// NewStagedDefinition validates a staged change: a version, when it was
// staged and a JSON object.
func NewStagedDefinition(version SemanticVersion, stagedAt time.Time, change []byte) (StagedDefinition, error) {
	if version.IsZero() {
		return StagedDefinition{}, invalid("staged version", "is required")
	}
	if _, err := ParseSemanticVersion("staged version", version.String()); err != nil {
		return StagedDefinition{}, err
	}
	if stagedAt.IsZero() {
		return StagedDefinition{}, invalid("staged at", "must not be zero")
	}
	trimmed := bytes.TrimSpace(change)
	if len(trimmed) > maxStagedChangeBytes {
		return StagedDefinition{}, invalid("staged change", "is larger than 1 MiB")
	}
	var fields map[string]json.RawMessage
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &fields) != nil {
		return StagedDefinition{}, invalid("staged change", "must be a JSON object")
	}
	return StagedDefinition{version: version, stagedAt: stagedAt.UTC(), change: bytes.Clone(trimmed)}, nil
}

// Version is the definition's version once the change is applied.
func (s StagedDefinition) Version() SemanticVersion { return s.version }

// StagedAt is when the change was staged.
func (s StagedDefinition) StagedAt() time.Time { return s.stagedAt }

// Change is the staged patch as JSON, a copy.
func (s StagedDefinition) Change() []byte { return bytes.Clone(s.change) }

// Snapshot is the staged change as plain data, the change included.
func (s StagedDefinition) Snapshot() StagedDefinitionSnapshot {
	return StagedDefinitionSnapshot{Version: s.version.String(), StagedAt: s.stagedAt, Change: s.Change()}
}

// RestoreStagedDefinition rebuilds a staged change from its snapshot.
func RestoreStagedDefinition(s StagedDefinitionSnapshot) (StagedDefinition, error) {
	version, err := ParseSemanticVersion("staged version", s.Version)
	if err != nil {
		return StagedDefinition{}, err
	}
	return NewStagedDefinition(version, s.StagedAt, s.Change)
}
