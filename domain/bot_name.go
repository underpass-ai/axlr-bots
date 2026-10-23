package domain

// BotName is the human name of a bot.
type BotName string

// NewBotName validates a bot name: 1 to 120 characters of text.
func NewBotName(raw string) (BotName, error) {
	value, err := validateText("bot name", raw, 120)
	if err != nil {
		return "", err
	}
	return BotName(value), nil
}

func (n BotName) String() string { return string(n) }
