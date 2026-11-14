package application

// activityChanges lets a watch of a task's activity wait on the same
// hub the long polls of the timelines wait on: every event recorded is a
// change.
type activityChanges struct{ hub *ChangeHub }

// Next answers the channel the next change closes.
func (a activityChanges) Next() <-chan struct{} {
	signal, _ := a.hub.Next()
	return signal
}

// ActivityChanges is what a watch of a task's activity waits on.
func (l *LocalOperator) ActivityChanges() ChangeSignal { return activityChanges{hub: l.changes} }
