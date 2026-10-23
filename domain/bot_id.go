package domain

// BotID identifies a bot.
type BotID string

// NewBotID validates a bot identifier.
func NewBotID(raw string) (BotID, error) {
	value, err := validateIdentifier("bot id", raw)
	if err != nil {
		return "", err
	}
	return BotID(value), nil
}

func (id BotID) String() string { return string(id) }
