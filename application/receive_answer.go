package application

// ReceiveAnswer is what a domain event did on the listening side: the
// tasks it made (or found made by the same fact before) and the teams or
// bots it did not reach, and why.
type ReceiveAnswer struct {
	Deliveries []EventDelivery
	Skipped    []EventSkip
}
