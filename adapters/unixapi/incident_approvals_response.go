package unixapi

// IncidentApprovalsResponse counts the approvals an incident's tasks
// waited for, by how each ended (a task keeps its last wait). The
// product's rule is one approval per incident; the control plane shows
// the count and does not enforce it.
type IncidentApprovalsResponse struct {
	Approved int `json:"approved"`
	Denied   int `json:"denied"`
	Expired  int `json:"expired"`
	Waiting  int `json:"waiting"`
}
