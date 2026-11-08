package domain

const maxLadderRunbooks = 32

// Ladder is a bot's opt-in to progressive autonomy on its apply trigger
// (axlr-sre design §8.3): the runbooks it may earn the top step for,
// each with its bounds, and how many signals that takes.
type Ladder struct {
	runbooks   []LadderRunbook
	thresholds AutonomyThresholds
}

// NewLadder validates a ladder: 1 to 32 runbooks, no name twice.
func NewLadder(s LadderSnapshot) (Ladder, error) {
	if len(s.Runbooks) == 0 || len(s.Runbooks) > maxLadderRunbooks {
		return Ladder{}, invalid("ladder", "names 1 to 32 runbooks")
	}
	l := Ladder{thresholds: DefaultAutonomyThresholds()}
	if s.Thresholds != nil {
		thresholds, err := NewAutonomyThresholds(*s.Thresholds)
		if err != nil {
			return Ladder{}, err
		}
		l.thresholds = thresholds
	}
	seen := map[RunbookName]bool{}
	for _, definition := range s.Runbooks {
		runbook, err := newLadderRunbook(definition)
		if err != nil {
			return Ladder{}, err
		}
		if seen[runbook.name] {
			return Ladder{}, invalid("ladder", "repeat the runbook "+runbook.name.String())
		}
		seen[runbook.name] = true
		l.runbooks = append(l.runbooks, runbook)
	}
	return l, nil
}

// Thresholds are how many signals the ladder's runbooks need.
func (l Ladder) Thresholds() AutonomyThresholds { return l.thresholds }

// Runbooks are the ladder's runbooks, copied.
func (l Ladder) Runbooks() []LadderRunbook { return append([]LadderRunbook(nil), l.runbooks...) }

// Runbook finds a runbook by name.
func (l Ladder) Runbook(name RunbookName) (LadderRunbook, bool) {
	for _, runbook := range l.runbooks {
		if runbook.name == name {
			return runbook, true
		}
	}
	return LadderRunbook{}, false
}

// Snapshot returns the ladder as plain data.
func (l Ladder) Snapshot() LadderSnapshot {
	thresholds := l.thresholds.Snapshot()
	s := LadderSnapshot{Thresholds: &thresholds}
	for _, runbook := range l.runbooks {
		s.Runbooks = append(s.Runbooks, runbook.snapshot())
	}
	return s
}
