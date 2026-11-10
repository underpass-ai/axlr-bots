package application

// AskTeamCommand is what a running execution asks through the node: that
// a member of the team answer the question. There is no waiting: the
// node answers the model at once, and the answer comes back later
// (docs/architecture/team-channel.md, amendment).
type AskTeamCommand struct {
	Team     string
	Question string
}
