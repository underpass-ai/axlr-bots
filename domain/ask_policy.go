package domain

import "slices"

// Limits and defaults of a bot's ask (docs/architecture/team-channel.md).
const (
	// DefaultAskPerMinute is how many questions a bot may ask a minute
	// when its ask names no rate.
	DefaultAskPerMinute = 2
	// MaxAskPerMinute is the highest rate an ask may name.
	MaxAskPerMinute = 10
	// DefaultAskMaxOpen is how many of a bot's questions may wait for an
	// answer at once when its ask names no limit.
	DefaultAskMaxOpen = 3
	// MaxAskMaxOpen is the highest such limit an ask may name.
	MaxAskMaxOpen = 8
)

// AskPolicy says which other teams a bot may ask for context, and how
// often. The zero value asks nobody: the bot is not offered the tool.
// That the bot names none of its own teams is Bot.CheckAsk's, since a
// bot's teams may change after its ask.
type AskPolicy struct {
	teams     []TeamName
	perMinute int
	maxOpen   int
}

// NewAskPolicy validates an ask: up to 8 distinct teams, a rate from 1
// to 10 a minute (0 when not given) and from 1 to 8 open questions (0
// when not given).
func NewAskPolicy(s AskSnapshot) (AskPolicy, error) {
	teams, err := parseTeamList("ask teams", s.Teams)
	if err != nil {
		return AskPolicy{}, err
	}
	if s.PerMinute < 0 || s.PerMinute > MaxAskPerMinute {
		return AskPolicy{}, invalid("ask perMinute", "must be from 1 to 10")
	}
	if s.MaxOpen < 0 || s.MaxOpen > MaxAskMaxOpen {
		return AskPolicy{}, invalid("ask maxOpen", "must be from 1 to 8")
	}
	return AskPolicy{teams: teams, perMinute: s.PerMinute, maxOpen: s.MaxOpen}, nil
}

// Teams lists the teams the bot may ask, in the order its definition
// gives them.
func (p AskPolicy) Teams() []TeamName { return slices.Clone(p.teams) }

// Enabled reports whether the bot may ask any team: it is offered the
// tool only then.
func (p AskPolicy) Enabled() bool { return len(p.teams) > 0 }

// Allows reports whether the bot may ask the team.
func (p AskPolicy) Allows(team TeamName) bool { return slices.Contains(p.teams, team) }

// PerMinute is how many questions the bot may ask a minute; the default
// when not given.
func (p AskPolicy) PerMinute() int {
	if p.perMinute == 0 {
		return DefaultAskPerMinute
	}
	return p.perMinute
}

// MaxOpen is how many of the bot's questions may wait for an answer at
// once; the default when not given.
func (p AskPolicy) MaxOpen() int {
	if p.maxOpen == 0 {
		return DefaultAskMaxOpen
	}
	return p.maxOpen
}

// Snapshot returns the ask as plain data, nil when it is empty, so a bot
// without one stores as it did before the team channel.
func (p AskPolicy) Snapshot() *AskSnapshot {
	if len(p.teams) == 0 && p.perMinute == 0 && p.maxOpen == 0 {
		return nil
	}
	return &AskSnapshot{Teams: teamStrings(p.teams), PerMinute: p.perMinute, MaxOpen: p.maxOpen}
}
