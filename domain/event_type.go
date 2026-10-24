package domain

// EventType names what happened. Names follow vision §11.2
// (bot.task.assigned): the subject first, then the fact.
type EventType string

const (
	// EventBotRegistered says a bot was registered.
	EventBotRegistered EventType = "bot.registered"
	// EventBotEnabled says a bot now accepts tasks.
	EventBotEnabled EventType = "bot.enabled"
	// EventTaskSubmitted says a task was accepted for a bot.
	EventTaskSubmitted EventType = "bot.task.submitted"
	// EventTaskStarted says an execution began for a task.
	EventTaskStarted EventType = "bot.task.started"
	// EventTaskCompleted says a task produced its result.
	EventTaskCompleted EventType = "bot.task.completed"
	// EventTaskFailed says a task's execution failed.
	EventTaskFailed EventType = "bot.task.failed"
	// EventTaskWaitingApproval says a task stopped on a decision for a person.
	EventTaskWaitingApproval EventType = "bot.task.waiting_approval"
	// EventTaskDecided says a person decided the pending call and the task
	// resumed under a new execution.
	EventTaskDecided EventType = "bot.task.decided"
	// EventNodeRegistered says a node registered (or registered again).
	EventNodeRegistered EventType = "node.registered"
	// EventNodeHeartbeat says a node was heard from.
	EventNodeHeartbeat EventType = "node.heartbeat"
	// EventNodeOffline says a node's heartbeat stopped.
	EventNodeOffline EventType = "node.offline"
	// EventNodeEnrolled says a node presented an enrollment token and
	// received its certificate.
	EventNodeEnrolled EventType = "node.enrolled"
	// EventNodeRevoked says the operator cut a node off.
	EventNodeRevoked EventType = "node.revoked"
	// EventExecutionAssigned says an execution was handed to a node.
	EventExecutionAssigned EventType = "execution.assigned"
	// EventExecutionReported says a node reported how an execution ended.
	EventExecutionReported EventType = "execution.reported"
)

// ParseEventType validates an event type.
func ParseEventType(raw string) (EventType, error) {
	switch EventType(raw) {
	case EventBotRegistered, EventBotEnabled, EventTaskSubmitted, EventTaskStarted,
		EventTaskCompleted, EventTaskFailed, EventTaskWaitingApproval, EventTaskDecided,
		EventNodeRegistered, EventNodeHeartbeat, EventNodeOffline, EventNodeEnrolled, EventNodeRevoked,
		EventExecutionAssigned, EventExecutionReported:
		return EventType(raw), nil
	}
	return "", invalid("event type", "is not a known event type")
}

func (t EventType) String() string { return string(t) }

// AboutNode reports an event about a node rather than a bot.
func (t EventType) AboutNode() bool {
	switch t {
	case EventNodeRegistered, EventNodeHeartbeat, EventNodeOffline, EventNodeEnrolled, EventNodeRevoked:
		return true
	}
	return false
}
