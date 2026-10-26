package domain

import (
	"fmt"
	"strings"
)

// MaxDirectoryMembers is the most teammates a directory lists.
const MaxDirectoryMembers = 32

// TeamDirectory is what a bot knows of its teams before a task: the
// teams and, sorted by id, its Enabled teammates. It goes in the prompt
// between the mission and the task, cut from the end to fit.
type TeamDirectory struct {
	teams   []TeamName
	members []Teammate
}

// NewTeamDirectory keeps at most 32 teammates, in the order given.
func NewTeamDirectory(teams []TeamName, members []Teammate) TeamDirectory {
	return TeamDirectory{teams: append([]TeamName(nil), teams...), members: append([]Teammate(nil), members...)}
}

// Empty reports whether the directory has nobody to list.
func (d TeamDirectory) Empty() bool { return len(d.members) == 0 }

// Members lists the teammates in the directory.
func (d TeamDirectory) Members() []Teammate { return append([]Teammate(nil), d.members...) }

// Render writes the directory in at most limit bytes: the heading, then
// one line per teammate while they fit, then how many it left out. An
// empty string when not even the heading fits or there is nobody.
func (d TeamDirectory) Render(limit int) string {
	if d.Empty() {
		return ""
	}
	names := make([]string, len(d.teams))
	for i, name := range d.teams {
		names[i] = name.String()
	}
	label := "team " + strings.Join(names, ", ")
	if len(names) > 1 {
		label = "teams " + strings.Join(names, ", ")
	}
	heading := "Your teammates (" + label + "), bots of your operator that work with you:\n"
	var b strings.Builder
	b.WriteString(heading)
	listed := 0
	for _, member := range d.members {
		if listed == MaxDirectoryMembers {
			break
		}
		line := member.Line(len(d.teams) > 1) + "\n"
		rest := len(d.members) - listed - 1
		reserve := 0
		if rest > 0 {
			reserve = len(fmt.Sprintf("… and %d more\n", rest))
		}
		if b.Len()+len(line)+reserve > limit {
			break
		}
		b.WriteString(line)
		listed++
	}
	if listed < len(d.members) {
		more := fmt.Sprintf("… and %d more\n", len(d.members)-listed)
		if b.Len()+len(more) > limit {
			return ""
		}
		b.WriteString(more)
	}
	if b.Len() > limit {
		return ""
	}
	return strings.TrimSuffix(b.String(), "\n")
}
