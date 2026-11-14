package application

// SessionItemReader reads the items of a run from the whole transcript of
// one of its runtime sessions, in the order they happened. The runtime's
// session format is its adapter's business.
type SessionItemReader interface {
	ReadItems(content []byte) ([]ProgressItem, error)
}
