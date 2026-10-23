package domain

// Mission is the bot's declarative responsibility: what it is for and
// where its remit ends.
type Mission string

// NewMission validates a mission: 1 to 2000 characters of text.
func NewMission(raw string) (Mission, error) {
	value, err := validateText("mission", raw, 2000)
	if err != nil {
		return "", err
	}
	return Mission(value), nil
}

func (m Mission) String() string { return string(m) }
