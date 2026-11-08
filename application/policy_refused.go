package application

// errPolicyRefused says a task cannot be handed to a node: nothing of
// its policy is left to run it under (domain.Bot.PolicyOf). The task
// fails with the reason rather than run with an empty allow-list.
type errPolicyRefused struct {
	cause error
}

func (e errPolicyRefused) Error() string { return "policy refused: " + e.cause.Error() }

func (e errPolicyRefused) Unwrap() error { return e.cause }
