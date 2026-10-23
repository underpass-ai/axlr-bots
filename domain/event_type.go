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
)

// ParseEventType validates an event type.
func ParseEventType(raw string) (EventType, error) {
	switch EventType(raw) {
	case EventBotRegistered, EventBotEnabled, EventTaskSubmitted, EventTaskStarted,
		EventTaskCompleted, EventTaskFailed, EventTaskWaitingApproval:
		return EventType(raw), nil
	}
	return "", invalid("event type", "is not a known event type")
}

func (t EventType) String() string { return string(t) }
