package unixapi

// PostmortemReviewRequest is a person's answer to an incident's
// postmortem (POST /v1/incidents/{id}/postmortem/approve and /send-back).
// DecidedBy is who answers, as the token holder asserts it (the
// X-Axlr-Actor header says the same); one of them is required. Note is
// what the task should change: required to send it back, not read on an
// approval.
type PostmortemReviewRequest struct {
	Note      string `json:"note,omitempty"`
	DecidedBy string `json:"decidedBy,omitempty"`
}
