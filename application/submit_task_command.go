package application

// SubmitTaskCommand asks for a task on a bot. The idempotency key makes
// the submission repeatable: the same bot and key name the same task.
type SubmitTaskCommand struct {
	BotID          string
	Objective      string
	IdempotencyKey string
	CorrelationID  string
}
