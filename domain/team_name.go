package domain

// TeamName names a team of bots: the operator's word for a group whose
// members know each other, may ask each other to do a task and hear each
// other's events. A team exists while a bot of the tenant names it.
type TeamName string

// NewTeamName validates a team's name: an identifier of up to 64
// lowercase letters, digits, '.', '_' and '-'.
func NewTeamName(raw string) (TeamName, error) {
	value, err := validateIdentifier("team", raw)
	if err != nil {
		return "", err
	}
	return TeamName(value), nil
}

func (n TeamName) String() string { return string(n) }
