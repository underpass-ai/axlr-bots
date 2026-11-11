package nodeproto

import (
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// TaskWatchMessage is where one of a watcher's team's own running tasks
// stands, from the control plane's records: what its Jev judges stuck or
// not.
type TaskWatchMessage struct {
	Task         string     `json:"task"`
	Team         string     `json:"team"`
	Bot          string     `json:"bot"`
	Trigger      string     `json:"trigger"`
	Objective    string     `json:"objective"`
	Now          time.Time  `json:"now"`
	RunningSince time.Time  `json:"runningSince"`
	Steps        int        `json:"steps"`
	LastStep     string     `json:"lastStep,omitempty"`
	LastStepAt   *time.Time `json:"lastStepAt,omitempty"`
	OpenTasks    int        `json:"openTasks"`
}

// FromTaskWatchView renders a task's state as a message.
func FromTaskWatchView(v application.TaskWatchView) TaskWatchMessage {
	m := TaskWatchMessage{
		Task: v.Task.String(), Team: v.Team.String(), Bot: v.Bot.String(), Trigger: v.Trigger, Objective: v.Objective,
		Now: v.Now, RunningSince: v.RunningSince, Steps: v.Steps, LastStep: v.LastStep, OpenTasks: v.OpenTasks,
	}
	if !v.LastStepAt.IsZero() {
		at := v.LastStepAt
		m.LastStepAt = &at
	}
	return m
}

// ToTaskWatchView reads a task's state back.
func ToTaskWatchView(m TaskWatchMessage) (application.TaskWatchView, error) {
	task, err := domain.NewTaskID(m.Task)
	if err != nil {
		return application.TaskWatchView{}, err
	}
	v := application.TaskWatchView{
		Task: task, Team: domain.TeamName(m.Team), Bot: domain.BotID(m.Bot), Trigger: m.Trigger, Objective: m.Objective,
		Now: m.Now, RunningSince: m.RunningSince, Steps: m.Steps, LastStep: m.LastStep, OpenTasks: m.OpenTasks,
	}
	if m.LastStepAt != nil {
		v.LastStepAt = *m.LastStepAt
	}
	return v, nil
}
