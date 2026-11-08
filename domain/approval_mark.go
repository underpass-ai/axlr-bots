package domain

import "time"

// ApprovalMark is one person's approval of a call that waits for two:
// who approved, as the decision named them, and when.
type ApprovalMark struct {
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
}
