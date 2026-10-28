package domain

// EventType names what happened. Names follow vision §11.2
// (bot.task.assigned): the subject first, then the fact.
type EventType string

const (
	// EventBotRegistered says a bot was registered.
	EventBotRegistered EventType = "bot.registered"
	// EventBotEnabled says a bot now accepts tasks.
	EventBotEnabled EventType = "bot.enabled"
	// EventBotUpdated says a bot's definition changed.
	EventBotUpdated EventType = "bot.updated"
	// EventBotPaused says an enabled bot was put on hold; it accepts no
	// task until it is enabled again.
	EventBotPaused EventType = "bot.paused"
	// EventTriggerFired says a trigger fired: a webhook arrived or a
	// timer's slot came. The payload names the task and says whether the
	// fire created it or found it under its idempotency key.
	EventTriggerFired EventType = "bot.trigger.fired"
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
	// EventTaskCancelled says the operator stopped the task.
	EventTaskCancelled EventType = "bot.task.cancelled"
	// EventTaskPreempted says an urgent task of the bot cancelled this
	// one, which was running or waiting for a person: the payload names
	// the urgent task and why it was urgent.
	EventTaskPreempted EventType = "bot.task.preempted"
	// EventTaskExpired says a task ended Expired: nobody decided the call
	// it waited on in time, and the call never ran.
	EventTaskExpired EventType = "bot.task.expired"
	// EventTaskDelegated says a task's execution asked another bot to do
	// a task and the control plane created it; the payload names the
	// child task and bot, the depth and the grant.
	EventTaskDelegated EventType = "bot.task.delegated"
	// EventTaskMessage says the operator wrote to a running task: the
	// payload holds the text, the event names the execution it goes to.
	EventTaskMessage EventType = "bot.task.message"
	// EventTaskSaid says a running task's execution answered the
	// operator's message: the payload holds its words and the message.
	EventTaskSaid EventType = "bot.task.said"
	// EventDelegationRefused says a task's execution asked another bot
	// and the control plane refused: the payload names the target and
	// the check that refused it.
	EventDelegationRefused EventType = "bot.delegation.refused"
	// EventEventEmitted says a bot told its team something happened: the
	// payload names the event, its severity, message and data, and who
	// got a task from it and who did not, and why.
	EventEventEmitted EventType = "bot.event.emitted"
	// EventEventHeard says a bot heard a teammate's event: the payload
	// names the event, who sent it and by what (a bot, the operator, the
	// control plane), the mode of the delivery and the task it made.
	EventEventHeard EventType = "bot.event.heard"
	// EventSubscriptionChanged says the topics a bot listens to changed:
	// the payload holds the list after the change and who changed it.
	EventSubscriptionChanged EventType = "bot.subscription.changed"
	// EventSubscriptionAsked says a bot asked to listen to a topic: the
	// payload names the topic and why; the subscription waits pending
	// for a person.
	EventSubscriptionAsked EventType = "bot.subscription.asked"
	// EventGroupEventPublished says the operator published an event on a
	// team: the payload names the event, whom it was addressed to and
	// who heard it and how. It names the team, not a bot.
	EventGroupEventPublished EventType = "group.event.published"
	// EventEventRefused says a bot tried to emit an event and the control
	// plane refused: the payload names the event and the check.
	EventEventRefused EventType = "bot.event.refused"
	// EventApprovalExpired says nobody decided a pending call by its
	// deadline: the call was denied or the task ended Expired, as the
	// bot's approvals say.
	EventApprovalExpired EventType = "bot.approval.expired"
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
	case EventBotRegistered, EventBotEnabled, EventBotUpdated, EventBotPaused, EventTriggerFired, EventTaskSubmitted, EventTaskStarted,
		EventTaskCompleted, EventTaskFailed, EventTaskWaitingApproval, EventTaskDecided, EventTaskCancelled, EventTaskPreempted, EventTaskExpired, EventApprovalExpired, EventTaskDelegated, EventTaskMessage, EventTaskSaid, EventDelegationRefused,
		EventEventEmitted, EventEventRefused, EventEventHeard, EventSubscriptionChanged, EventSubscriptionAsked, EventGroupEventPublished,
		EventNodeRegistered, EventNodeHeartbeat, EventNodeOffline, EventNodeEnrolled, EventNodeRevoked,
		EventExecutionAssigned, EventExecutionReported:
		return EventType(raw), nil
	}
	return "", invalid("event type", "is not a known event type")
}

func (t EventType) String() string { return string(t) }

// AboutGroup reports an event about a team rather than a bot.
func (t EventType) AboutGroup() bool { return t == EventGroupEventPublished }

// AboutNode reports an event about a node rather than a bot.
func (t EventType) AboutNode() bool {
	switch t {
	case EventNodeRegistered, EventNodeHeartbeat, EventNodeOffline, EventNodeEnrolled, EventNodeRevoked:
		return true
	}
	return false
}
