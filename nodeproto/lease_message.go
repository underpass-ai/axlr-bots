package nodeproto

import "time"

// LeaseMessage is a renewed lease's expiry.
type LeaseMessage struct {
	LeaseUntil time.Time `json:"leaseUntil"`
}
