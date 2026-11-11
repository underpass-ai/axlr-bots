package nodeproto

// WatchDecisionMessage is what a node sends when a team's Jev watcher
// decided for the question it was put: Jev's choice, wait or abandon, and
// its verdict.
type WatchDecisionMessage struct {
	Question    string  `json:"question"`
	Choice      string  `json:"choice"`
	Probability float64 `json:"probability"`
	Confidence  float64 `json:"confidence"`
}
