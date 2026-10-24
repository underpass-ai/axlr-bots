package domain

// TriggerType is a way a task reaches a bot. Manual is a submission by
// the API or the command; the other types (a systemd unit event, a timer,
// a webhook) come with their routers.
type TriggerType string

const (
	// TriggerManual is a task submitted by a person or a script.
	TriggerManual TriggerType = "manual"
)

// ParseTriggerType validates a trigger type.
func ParseTriggerType(raw string) (TriggerType, error) {
	switch TriggerType(raw) {
	case TriggerManual:
		return TriggerType(raw), nil
	}
	return "", invalid("trigger type", "is not a known trigger type (manual)")
}

func (t TriggerType) String() string { return string(t) }
