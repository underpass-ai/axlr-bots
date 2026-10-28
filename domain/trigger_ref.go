package domain

// TriggerRef names the trigger a task came through: its type and name.
type TriggerRef struct {
	Type TriggerType `json:"type"`
	Name string      `json:"name"`
}

// ManualTrigger is the reference of a task submitted by hand.
func ManualTrigger() TriggerRef { return TriggerRef{Type: TriggerManual, Name: TriggerManual.String()} }

// DelegationTrigger is the reference of a task another bot asked for.
func DelegationTrigger() TriggerRef {
	return TriggerRef{Type: TriggerDelegation, Name: TriggerDelegation.String()}
}

// NewTriggerRef validates a reference; an empty one is the manual trigger.
func NewTriggerRef(kind, name string) (TriggerRef, error) {
	if kind == "" && name == "" {
		return ManualTrigger(), nil
	}
	parsed, err := ParseTriggerType(kind)
	if err != nil {
		return TriggerRef{}, err
	}
	if name == "" {
		name = parsed.String()
	}
	identifier, err := validateIdentifier("trigger name", name)
	if err != nil && parsed == TriggerEvent {
		// A task an event made names the subscription's pattern; one made
		// before subscriptions names its event trigger.
		pattern, perr := NewEventPattern(name)
		if perr != nil {
			return TriggerRef{}, err
		}
		return TriggerRef{Type: parsed, Name: pattern.String()}, nil
	}
	if err != nil {
		return TriggerRef{}, err
	}
	name = identifier
	return TriggerRef{Type: parsed, Name: name}, nil
}
