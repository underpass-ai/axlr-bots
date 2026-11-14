package application

// ErrorClassification is how an error is reported to an operator: the
// kind names the domain error so a client can rebuild it, the message is
// for people. A not-found error also carries what was looked for, and a
// forbidden one whether the refusal is for good.
type ErrorClassification struct {
	Kind     string
	Message  string
	Resource string
	ID       string
	// Revoked marks a forbidden answer to a revoked node: refused for
	// good, until it is enrolled again.
	Revoked bool
}
