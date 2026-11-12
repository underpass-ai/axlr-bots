package domain

import (
	"encoding/json"
	"time"
)

// StagedDefinitionSnapshot is a bot's staged change as plain data. The
// store keeps the change; an answer about the bot leaves it out
// (BotSnapshot.Redacted), since it may carry trigger and notification
// secrets, and says only which version waits and since when.
type StagedDefinitionSnapshot struct {
	Version  string          `json:"version"`
	StagedAt time.Time       `json:"stagedAt"`
	Change   json.RawMessage `json:"change,omitempty"`
}
