package domain

// BotStatus is the logical state of a bot. It says nothing about whether a
// process is running: an Enabled bot may have no execution at all.
type BotStatus string

const (
	// BotDraft is a bot defined but not yet registered.
	BotDraft BotStatus = "Draft"
	// BotRegistered is a bot the control plane knows but will not run tasks for.
	BotRegistered BotStatus = "Registered"
	// BotEnabled is a bot that accepts tasks.
	BotEnabled BotStatus = "Enabled"
	// BotPaused is an enabled bot put on hold; it keeps its tasks.
	BotPaused BotStatus = "Paused"
)

// ParseBotStatus validates a bot status.
func ParseBotStatus(raw string) (BotStatus, error) {
	switch BotStatus(raw) {
	case BotDraft, BotRegistered, BotEnabled, BotPaused:
		return BotStatus(raw), nil
	}
	return "", invalid("bot status", "must be Draft, Registered, Enabled or Paused")
}

func (s BotStatus) String() string { return string(s) }
