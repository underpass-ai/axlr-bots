package domain

import "slices"

// Audience is the teams a domain event is published to: the teams the
// emitter's rule names, at least one, at most 8, each once, never the
// emitting team itself (docs/architecture/team-topics.md). It is not part
// of the event's identity: the same fact is one event whoever hears it.
type Audience struct {
	teams []TeamName
}

// NewAudience validates the teams the emitting team publishes an event to.
func NewAudience(emitter TeamName, teams []TeamName) (Audience, error) {
	raw := make([]string, len(teams))
	for i, team := range teams {
		raw[i] = team.String()
	}
	return ParseAudience(emitter, raw)
}

// ParseAudience reads the audience from plain names.
func ParseAudience(emitter TeamName, raw []string) (Audience, error) {
	if len(raw) == 0 {
		return Audience{}, invalid("audience", "names at least one team")
	}
	teams, err := parseTeamList("audience", raw)
	if err != nil {
		return Audience{}, err
	}
	if slices.Contains(teams, emitter) {
		return Audience{}, invalid("audience", "names "+emitter.String()+", the emitting team: its own members hear it already")
	}
	return Audience{teams: teams}, nil
}

// Teams are the teams, in the order the emitter named them.
func (a Audience) Teams() []TeamName { return append([]TeamName(nil), a.teams...) }

// Strings are the teams as plain names.
func (a Audience) Strings() []string { return teamStrings(a.teams) }

// Has reports whether the team is in the audience.
func (a Audience) Has(team TeamName) bool { return slices.Contains(a.teams, team) }
