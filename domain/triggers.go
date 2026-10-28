package domain

// Triggers are the ways tasks reach a bot. A bot that declares none takes
// manual tasks; one that declares some takes only those. Names are
// unique within the bot.
type Triggers struct {
	list []Trigger
}

// DefaultTriggers is what a definition without triggers means: manual.
func DefaultTriggers() Triggers {
	return Triggers{list: []Trigger{{kind: TriggerManual, name: TriggerManual.String()}}}
}

// NewTriggers validates the list and refuses a repeated name.
func NewTriggers(definitions []TriggerSnapshot) (Triggers, error) {
	if len(definitions) == 0 {
		return DefaultTriggers(), nil
	}
	seen := map[string]bool{}
	t := Triggers{}
	for _, definition := range definitions {
		trigger, err := NewTrigger(definition)
		if err != nil {
			return Triggers{}, err
		}
		if seen[trigger.Name()] {
			return Triggers{}, invalid("triggers", "repeat the name "+trigger.Name())
		}
		seen[trigger.Name()] = true
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

// Named finds the trigger by name.
func (t Triggers) Named(name string) (Trigger, bool) {
	for _, trigger := range t.list {
		if trigger.Name() == name {
			return trigger, true
		}
	}
	return Trigger{}, false
}

// List returns the triggers, copied.
func (t Triggers) List() []Trigger { return append([]Trigger(nil), t.list...) }

// Timers returns the timer triggers.
func (t Triggers) Timers() []Trigger {
	var out []Trigger
	for _, trigger := range t.list {
		if trigger.Type() == TriggerTimer {
			out = append(out, trigger)
		}
	}
	return out
}

// Snapshot returns the triggers as plain data.
func (t Triggers) Snapshot() []TriggerSnapshot {
	out := make([]TriggerSnapshot, 0, len(t.list))
	for _, trigger := range t.list {
		out = append(out, trigger.Snapshot())
	}
	return out
}
