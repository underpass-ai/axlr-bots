package topicwire

// Address is what a publication's subject names: the tenant's and the
// team's tokens and the event's name.
type Address struct {
	TenantToken string
	TeamToken   string
	Name        string
}
