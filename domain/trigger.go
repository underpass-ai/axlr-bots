package domain

// Trigger is one way tasks reach the bot, as its definition declares it.
type Trigger struct {
	kind TriggerType
}

// NewTrigger validates a trigger.
func NewTrigger(kind string) (Trigger, error) {
	parsed, err := ParseTriggerType(kind)
	if err != nil {
		return Trigger{}, err
	}
	return Trigger{kind: parsed}, nil
}

// Type is the trigger's type.
func (t Trigger) Type() TriggerType { return t.kind }

// Snapshot returns the trigger as plain data.
func (t Trigger) Snapshot() TriggerSnapshot { return TriggerSnapshot{Type: t.kind.String()} }
