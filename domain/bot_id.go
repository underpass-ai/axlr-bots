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

// ChannelBotID is the id the team channel writes a team's exchanges
// under (about project:<team>.canal), so no bot may have it: a bot named
// canal could write the channel's record as its own.
const ChannelBotID BotID = "canal"

// Reserved reports an id no bot may take.
func (id BotID) Reserved() bool { return id == ChannelBotID }
