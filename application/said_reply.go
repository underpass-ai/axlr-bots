package application

// SaidReply is what a run said back to an operator's message before it
// ended: the message it answers, and its words.
type SaidReply struct {
	InReplyTo string `json:"inReplyTo,omitempty"`
	Text      string `json:"text"`
}

// MaxSaidLength bounds the words one reply records; the rest goes with
// the run's answer.
const MaxSaidLength = 8000
