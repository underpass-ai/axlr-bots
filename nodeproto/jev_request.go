package nodeproto

// JevRequest is one choice a node without a Jev key puts to
// POST /v1/nodes/{id}/executions/{exec}/jev.
type JevRequest struct {
	State    string   `json:"state"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
}
