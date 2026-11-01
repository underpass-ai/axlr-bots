package application

// IncidentQuery selects the incidents of a list. Every field is
// optional: no tenant means every tenant, no status every status, and a
// Limit of zero DefaultTaskLimit.
type IncidentQuery struct {
	Tenant string
	Status string
	Limit  int
}
