package application

// GroupLink is one link of the team channel as the operator reads it
// (docs/architecture/team-channel.md, "Permissions and limits"): team
// From may ask team To, because some bot of From names To in its
// ask.teams (the askers) and some bot of To answers From through its
// question trigger (the responders). It is derived from the bots, as a
// team is; both lists are sorted by id and never empty.
type GroupLink struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Askers     []string `json:"askers"`
	Responders []string `json:"responders"`
}
