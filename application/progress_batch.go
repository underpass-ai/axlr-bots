package application

// ProgressBatch is what a node sends about a running execution at once:
// the items it read since the last batch, oldest first, and how many it
// dropped because the control plane was slower than the run.
type ProgressBatch struct {
	Items   []ProgressItem `json:"items"`
	Dropped int            `json:"dropped,omitempty"`
}
