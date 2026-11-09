package domain

// MaxSkillPins bounds a bot's skills: what AXLR takes for one execution.
const MaxSkillPins = 32

// SkillPins are the catalog skills a bot is given, or a task was launched
// with. The zero value is none.
type SkillPins struct {
	pins []SkillPin
}

// NewSkillPins validates a list: at most MaxSkillPins, and one version of
// a skill per catalog.
func NewSkillPins(list []SkillPinSnapshot) (SkillPins, error) {
	if len(list) > MaxSkillPins {
		return SkillPins{}, invalid("skills", "a bot takes at most 32 skills")
	}
	pins := make([]SkillPin, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, snapshot := range list {
		pin, err := NewSkillPin(snapshot)
		if err != nil {
			return SkillPins{}, err
		}
		key := pin.Catalog() + "/" + pin.Name()
		if seen[key] {
			return SkillPins{}, invalid("skills", key+" is pinned twice")
		}
		seen[key] = true
		pins = append(pins, pin)
	}
	return SkillPins{pins: pins}, nil
}

// IsEmpty reports no skill at all.
func (s SkillPins) IsEmpty() bool { return len(s.pins) == 0 }

// All lists the pins, copied.
func (s SkillPins) All() []SkillPin { return append([]SkillPin(nil), s.pins...) }

// Snapshot is the list as plain data; nil for none, so a bot or a task
// without skills is stored as it always was.
func (s SkillPins) Snapshot() []SkillPinSnapshot {
	if s.IsEmpty() {
		return nil
	}
	list := make([]SkillPinSnapshot, len(s.pins))
	for i, pin := range s.pins {
		list[i] = pin.Snapshot()
	}
	return list
}
