package domain

// Triggers are the ways tasks reach a bot. A bot that declares none takes
// manual tasks; one that declares some takes only those.
type Triggers struct {
	list []Trigger
}

// DefaultTriggers is what a definition without triggers means: manual.
func DefaultTriggers() Triggers {
	return Triggers{list: []Trigger{{kind: TriggerManual}}}
}

// NewTriggers validates the list and refuses a repeated type.
func NewTriggers(kinds []string) (Triggers, error) {
	if len(kinds) == 0 {
		return DefaultTriggers(), nil
	}
	seen := map[TriggerType]bool{}
	t := Triggers{}
	for _, kind := range kinds {
		trigger, err := NewTrigger(kind)
		if err != nil {
			return Triggers{}, err
		}
		if seen[trigger.Type()] {
			return Triggers{}, invalid("triggers", "repeat "+trigger.Type().String())
		}
		seen[trigger.Type()] = true
		t.list = append(t.list, trigger)
	}
	return t, nil
}

// Has reports whether the bot takes tasks of the type.
func (t Triggers) Has(kind TriggerType) bool {
	for _, trigger := range t.list {
		if trigger.Type() == kind {
			return true
		}
	}
	return false
}

// List returns the triggers, copied.
func (t Triggers) List() []Trigger { return append([]Trigger(nil), t.list...) }

// Snapshot returns the triggers as plain data.
func (t Triggers) Snapshot() []TriggerSnapshot {
	out := make([]TriggerSnapshot, 0, len(t.list))
	for _, trigger := range t.list {
		out = append(out, trigger.Snapshot())
	}
	return out
}
