package nodeproto

// ReportAck answers a report: whether this report ended the execution
// (false when it had ended already) and the execution's outcome.
type ReportAck struct {
	Accepted bool   `json:"accepted"`
	Outcome  string `json:"outcome"`
}
