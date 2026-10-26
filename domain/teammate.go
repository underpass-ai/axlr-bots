package domain

import "strings"

// Teammate is one line of a bot's team directory: another bot of its
// teams as the bot reads it before a task, what it is for, its tools,
// where it runs and whether it takes a task the bot delegates.
type Teammate struct {
	ID              BotID
	Teams           []TeamName
	Purpose         string
	Tools           []string
	Where           string
	TakesDelegation bool
}

// TeammateOf describes other as bot's teammate: the teams they share,
// the first sentence of its mission, its tools, its placement and its
// consent to bot's delegations.
func TeammateOf(bot, other Bot) Teammate {
	return Teammate{
		ID: other.ID(), Teams: bot.Teams().Shared(other.Teams()), Purpose: other.Mission().Purpose(),
		Tools: other.AllowedTools().Names(), Where: other.Placement().Where(), TakesDelegation: other.TakesDelegationFrom(bot),
	}
}

// Line renders the teammate on one line of the directory; the teams are
// named only when the bot has more than one.
func (m Teammate) Line(manyTeams bool) string {
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(m.ID.String())
	if manyTeams {
		names := make([]string, len(m.Teams))
		for i, name := range m.Teams {
			names[i] = name.String()
		}
		b.WriteString(" (" + strings.Join(names, ", ") + ")")
	}
	b.WriteString(": " + m.Purpose)
	if len(m.Tools) > 0 {
		b.WriteString(" Tools: " + strings.Join(m.Tools, ", ") + ".")
	}
	b.WriteString(" Runs on " + m.Where + ".")
	if m.TakesDelegation {
		b.WriteString(" Takes delegated tasks.")
	}
	return b.String()
}
