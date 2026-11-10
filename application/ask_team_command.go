package application

// AskTeamCommand is what a running execution asks through the node: that
// a member of the team answer the question. Wait says whether the asker
// waits for the answer; the control plane records it on the question so
// the answer can come back the way the asker chose.
type AskTeamCommand struct {
	Team     string
	Question string
	Wait     bool
}
