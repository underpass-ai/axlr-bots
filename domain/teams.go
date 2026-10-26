package domain

import "slices"

// MaxTeamsPerBot is the most teams one bot may belong to.
const MaxTeamsPerBot = 8

// Teams are the teams a bot belongs to, sorted and without repeats. The
// zero value is no team: the bot knows nobody and nobody knows it.
type Teams struct {
	names []TeamName
}

// NewTeams validates the teams of a bot: up to 8 distinct names.
func NewTeams(raw []string) (Teams, error) {
	if len(raw) > MaxTeamsPerBot {
		return Teams{}, invalid("groups", "names at most 8 teams")
	}
	t := Teams{}
	for _, value := range raw {
		name, err := NewTeamName(value)
		if err != nil {
			return Teams{}, invalid("groups", err.Error())
		}
		if slices.Contains(t.names, name) {
			return Teams{}, invalid("groups", "repeats "+name.String())
		}
		t.names = append(t.names, name)
	}
	slices.Sort(t.names)
	return t, nil
}

// Names lists the teams, sorted.
func (t Teams) Names() []TeamName { return slices.Clone(t.names) }

// Empty reports whether the bot belongs to no team.
func (t Teams) Empty() bool { return len(t.names) == 0 }

// Has reports whether the bot belongs to the team.
func (t Teams) Has(name TeamName) bool { return slices.Contains(t.names, name) }

// Shared lists the teams both belong to, sorted.
func (t Teams) Shared(other Teams) []TeamName {
	var shared []TeamName
	for _, name := range t.names {
		if other.Has(name) {
			shared = append(shared, name)
		}
	}
	return shared
}

// Snapshot returns the teams as plain names, nil when there are none.
func (t Teams) Snapshot() []string {
	if len(t.names) == 0 {
		return nil
	}
	names := make([]string, len(t.names))
	for i, name := range t.names {
		names[i] = name.String()
	}
	return names
}
