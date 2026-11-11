package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// maxWatchObjective bounds the objective a watcher's view shows.
const maxWatchObjective = 200

// TaskWatchView is where one of a team's own running tasks stands, read
// from the control plane's records only: the bot, what it was asked, since
// when it runs, the steps its run reported and the newest of them, and the
// bot's other open tasks. A team's Jev watcher reads it to judge whether
// the task is stuck (docs/architecture/team-subscriptions.md, stuck work).
type TaskWatchView struct {
	Task      domain.TaskID
	Team      domain.TeamName
	Bot       domain.BotID
	Trigger   string
	Objective string
	Now       time.Time
	// RunningSince is when its run began.
	RunningSince time.Time
	// Steps counts the steps its run reported; LastStep is the newest,
	// and when.
	Steps      int
	LastStep   string
	LastStepAt time.Time
	// OpenTasks are the bot's other tasks not yet ended.
	OpenTasks int
}

// Text is the view in a few lines, for a judge or a person.
func (v TaskWatchView) Text() string {
	age := func(at time.Time) string { return v.Now.Sub(at).Truncate(time.Minute).String() }
	var b strings.Builder
	fmt.Fprintf(&b, "Task %s of bot %s in team %s, started by %s, running for %s.", v.Task, v.Bot, v.Team, v.Trigger, age(v.RunningSince))
	fmt.Fprintf(&b, "\nIt was asked: %s", v.Objective)
	if v.LastStep == "" {
		b.WriteString("\nIts run has reported no step.")
	} else {
		fmt.Fprintf(&b, "\nIts run reported %d steps; the last one %s ago: %s.", v.Steps, age(v.LastStepAt), v.LastStep)
	}
	fmt.Fprintf(&b, "\nBot %s has %d other open tasks.", v.Bot, v.OpenTasks)
	return b.String()
}

// readTaskWatchView builds the view of a running task, given since when
// it runs.
func readTaskWatchView(ctx context.Context, deps Dependencies, team domain.TeamName, task domain.Task, since time.Time) (TaskWatchView, error) {
	objective := []rune(strings.Join(strings.Fields(task.Objective().String()), " "))
	if len(objective) > maxWatchObjective {
		objective = append(objective[:maxWatchObjective], '…')
	}
	trigger := task.Trigger().Type.String()
	if name := task.Trigger().Name; name != "" && name != trigger {
		trigger += " " + name
	}
	v := TaskWatchView{Task: task.ID(), Team: team, Bot: task.BotID(), Trigger: trigger, Objective: string(objective), Now: deps.Clock.Now(), RunningSince: since}
	events, err := deps.EventReader.ListTaskEvents(ctx, task.ID(), MaxEventLimit)
	if err != nil {
		return TaskWatchView{}, err
	}
	for _, event := range events {
		if event.Type == domain.EventTaskProgress {
			items, _ := event.Payload["items"].([]any)
			v.Steps += len(items)
		}
	}
	if v.LastStep, v.LastStepAt, err = lastStep(ctx, deps, task.ID()); err != nil {
		return TaskWatchView{}, err
	}
	if v.OpenTasks, err = openTasksBesides(ctx, deps, task); err != nil {
		return TaskWatchView{}, err
	}
	return v, nil
}
