package domain

// Owner names the team or person responsible for a bot.
type Owner string

// NewOwner validates an owner: 1 to 120 characters of text.
func NewOwner(raw string) (Owner, error) {
	value, err := validateText("owner", raw, 120)
	if err != nil {
		return "", err
	}
	return Owner(value), nil
}

func (o Owner) String() string { return string(o) }
