package application

// IDGenerator mints unique identifiers with a prefix (bot-, task-, exec-,
// evt-, aud-) that are valid domain identifiers.
type IDGenerator interface {
	NewID(prefix string) string
}
