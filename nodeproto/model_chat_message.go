package nodeproto

// ModelChatMessage is one turn of what a node asks the model through the
// control plane.
type ModelChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
