package application

// Group is a team as the operator reads it (GET /v1/groups): its tenant,
// its name and its members, sorted by id. It is derived from the bots: a
// team exists while a bot of the tenant names it in its groups.
type Group struct {
	TenantID string        `json:"tenantId"`
	Name     string        `json:"name"`
	Members  []GroupMember `json:"members"`
}
