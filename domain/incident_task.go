package domain

// IncidentTask is one task that worked on an incident: which, by which
// bot, for which phase and in which cycle. A cycle has at most one task
// per phase.
type IncidentTask struct {
	task  TaskID
	bot   BotID
	phase IncidentPhase
	cycle int
}

// NewIncidentTask validates a task of an incident.
func NewIncidentTask(task TaskID, bot BotID, phase IncidentPhase, cycle int) (IncidentTask, error) {
	if task == "" || bot == "" {
		return IncidentTask{}, invalid("incident task", "needs a task and a bot")
	}
	if phase != IncidentPhaseTriage && phase != IncidentPhaseFix {
		return IncidentTask{}, invalid("incident task", "needs a phase, triage or fix")
	}
	if cycle < 1 {
		return IncidentTask{}, invalid("incident task", "cycle must be at least 1")
	}
	return IncidentTask{task: task, bot: bot, phase: phase, cycle: cycle}, nil
}

// TaskID is the task.
func (t IncidentTask) TaskID() TaskID { return t.task }

// BotID is the bot that ran the task.
func (t IncidentTask) BotID() BotID { return t.bot }

// Phase is the incident's step the task worked on.
func (t IncidentTask) Phase() IncidentPhase { return t.phase }

// Cycle is the incident's cycle the task belongs to.
func (t IncidentTask) Cycle() int { return t.cycle }

// Snapshot returns the task as plain data.
func (t IncidentTask) Snapshot() IncidentTaskSnapshot {
	return IncidentTaskSnapshot{TaskID: t.task.String(), BotID: t.bot.String(), Phase: t.phase.String(), Cycle: t.cycle}
}

// restoreIncidentTask rebuilds a task of an incident from a snapshot.
func restoreIncidentTask(s IncidentTaskSnapshot) (IncidentTask, error) {
	task, err := NewTaskID(s.TaskID)
	if err != nil {
		return IncidentTask{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return IncidentTask{}, err
	}
	phase, err := ParseIncidentPhase(s.Phase)
	if err != nil {
		return IncidentTask{}, err
	}
	return NewIncidentTask(task, bot, phase, s.Cycle)
}
