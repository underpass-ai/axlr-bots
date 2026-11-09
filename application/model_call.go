package application

// ModelCall is one request to a language model on a node's behalf.
type ModelCall struct {
	Model    string
	Messages []ModelChat
	Tools    []ModelToolSpec
}
