package nodeproto

// JevReply is Jev's answer to a JevRequest.
type JevReply struct {
	Choice      string  `json:"choice"`
	Probability float64 `json:"probability"`
	Confidence  float64 `json:"confidence"`
}
