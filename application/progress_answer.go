package application

// ProgressAnswer is the control plane's answer to a progress batch:
// whether it recorded it, and whether the execution has reached its
// bound, after which the node sends no more.
type ProgressAnswer struct {
	Recorded bool `json:"recorded"`
	Full     bool `json:"full,omitempty"`
}
