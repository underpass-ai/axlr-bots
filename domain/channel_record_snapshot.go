package domain

// ChannelRecordSnapshot is a team channel's record as plain data: what
// the control plane hands a node to write into a team's store, and what
// an event keeps of it.
type ChannelRecordSnapshot struct {
	Side      string `json:"side"`
	Team      string `json:"team"`
	Other     string `json:"otherTeam"`
	Question  string `json:"questionTaskId"`
	Asker     string `json:"asker"`
	Responder string `json:"responder"`
	Text      string `json:"question"`
	Answered  bool   `json:"answered"`
	Answer    string `json:"answer,omitempty"`
	Status    string `json:"status"`
}
