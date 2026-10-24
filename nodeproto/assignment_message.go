package nodeproto

import "time"

// AssignmentMessage is one execution handed to a node: a run or a
// decision, and the lease the node must renew while it works.
type AssignmentMessage struct {
	ExecutionID string         `json:"executionId"`
	Kind        string         `json:"kind"`
	LeaseUntil  time.Time      `json:"leaseUntil"`
	Run         *RunMessage    `json:"run,omitempty"`
	Decide      *DecideMessage `json:"decide,omitempty"`
}
