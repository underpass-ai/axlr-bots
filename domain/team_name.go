package domain

import "slices"

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

// maxTeamList is the most teams a bot's ask or a question trigger's from
// may name.
const maxTeamList = 8

// parseTeamList validates a list of up to 8 distinct team names, kept in
// the order given.
func parseTeamList(field string, raw []string) ([]TeamName, error) {
	if len(raw) > maxTeamList {
		return nil, invalid(field, "names at most 8 teams")
	}
	var names []TeamName
	for _, value := range raw {
		name, err := NewTeamName(value)
		if err != nil {
			return nil, invalid(field, err.Error())
		}
		if slices.Contains(names, name) {
			return nil, invalid(field, "repeats "+name.String())
		}
		names = append(names, name)
	}
	return names, nil
}

// teamStrings returns the names as plain strings, nil when there are none.
func teamStrings(names []TeamName) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = name.String()
	}
	return out
}
