package domain

// TriggerType is a way a task reaches a bot. Manual is a submission by
// the API or the command; a webhook is a request from outside with the
// trigger's secret; a timer is the control plane's own scheduler; a
// delegation is another bot's execution asking; an event is a
// teammate's execution telling its team something happened.
type TriggerType string

const (
	// TriggerManual is a task submitted by a person or a script.
	TriggerManual TriggerType = "manual"
	// TriggerWebhook is a task created by POST /v1/triggers/{bot}/{trigger}
	// from an event whose body becomes the task's input.
	TriggerWebhook TriggerType = "webhook"
	// TriggerTimer is a task created on a schedule by the daemon.
	TriggerTimer TriggerType = "timer"
	// TriggerDelegation is a task another bot's execution asked for: a
	// bot takes them only when it declares this trigger, its consent.
	TriggerDelegation TriggerType = "delegation"
	// TriggerEvent is a task a teammate's event made: the bot hears the
	// events its event triggers name or, declaring none, the ones
	// addressed to it and the team's warnings.
	TriggerEvent TriggerType = "event"
	// TriggerIncident is the triage task of an incident the control plane
	// opened: a bot takes them only when it declares this trigger, its
	// consent, with an optional objective template over the incident.
	TriggerIncident TriggerType = "incident"
)

// ParseTriggerType validates a trigger type.
func ParseTriggerType(raw string) (TriggerType, error) {
	switch TriggerType(raw) {
	case TriggerManual, TriggerWebhook, TriggerTimer, TriggerDelegation, TriggerEvent, TriggerIncident:
		return TriggerType(raw), nil
	}
	return "", invalid("trigger type", "is not a known trigger type (manual, webhook, timer, delegation, event, incident)")
}

func (t TriggerType) String() string { return string(t) }
