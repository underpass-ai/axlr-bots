package topicwire

// Publication is a publication read off the wire: where its subject
// points, its idempotency key and what it says.
type Publication struct {
	Address Address
	Key     string
	DTO     PublicationDTO
}
