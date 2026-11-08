package application

// AutonomyQuery narrows a listing of runbooks; empty fields match all.
type AutonomyQuery struct {
	Tenant    string
	Bot       string
	Runbook   string
	Namespace string
}
