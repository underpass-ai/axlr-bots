package unixapi

// DiagnosisRequest is a person's verdict on an incident's diagnosis:
// correct or wrong. MarkedBy is who marks it, as the token holder asserts
// it (the X-Axlr-Actor header says the same; both given must agree).
type DiagnosisRequest struct {
	Verdict  string `json:"verdict"`
	MarkedBy string `json:"markedBy,omitempty"`
}
