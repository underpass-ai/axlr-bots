package activityaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// DefaultPerMinute is how many events the adapter publishes a minute at
// most: a listening team settles a bounded number of events per task and
// the platform redelivers at most twice, so a first --since backfill must
// not flood it.
const DefaultPerMinute = 6

// DryRunEventID is the event id a dry run records for what it would have
// published.
const DryRunEventID = "dry-run"

// AuditorConfig is what an Auditor needs. Publisher nil is a dry run:
// nothing is published, and what would be is recorded as DryRunEventID.
// PerMinute bounds the publishes in any minute (0: no bound); a flush
// that reaches it waits, through Sleep, for the oldest of them to leave
// the minute.
// Team is the team the events are published to and Event their name,
// both the operator's choice (the spec's activityFailure).
// IgnorePrograms are local programs whose failures are not audited;
// IgnoreTeamPartial extends them to a local_exec whose line hid its
// program, by a member of Team (a team that runs probes of its own reports
// what they find itself).
type AuditorConfig struct {
	Source            Source
	Publisher         Publisher
	Store             *SeenStore
	Team              string
	Event             string
	IgnorePrograms    []string
	IgnoreTeamPartial bool
	PerMinute         int
	Now               func() time.Time
	Sleep             func(ctx context.Context, d time.Duration) error
	Log               *log.Logger
}

// Auditor audits finished tasks into the seen-store and publishes the
// signatures due. It is not safe for concurrent use: the watcher calls it
// from one goroutine.
type Auditor struct {
	config AuditorConfig
	teams  map[string]string
	// sent are the times of the publishes of the last minute, oldest first.
	sent []time.Time
}

// NewAuditor checks the configuration.
func NewAuditor(config AuditorConfig) (*Auditor, error) {
	if config.Source == nil || config.Store == nil {
		return nil, fmt.Errorf("activityaudit: an auditor needs a source and a store")
	}
	if _, err := domain.NewTeamName(config.Team); err != nil {
		return nil, err
	}
	if _, err := domain.NewEventName(config.Event); err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Log == nil {
		config.Log = log.New(io.Discard, "", 0)
	}
	if config.Sleep == nil {
		config.Sleep = sleep
	}
	if config.PerMinute < 0 {
		return nil, fmt.Errorf("activityaudit: at most %d events a minute", config.PerMinute)
	}
	return &Auditor{config: config, teams: map[string]string{}}, nil
}

// SetTeams records each bot's team from the teams the control plane
// lists: the first, by name, of a bot in several.
func (a *Auditor) SetTeams(groups []application.Group) {
	teams := map[string]string{}
	names := make([]string, 0, len(groups))
	byName := map[string]application.Group{}
	for _, group := range groups {
		names = append(names, group.Name)
		byName[group.Name] = group
	}
	slices.Sort(names)
	for _, name := range names {
		for _, member := range byName[name].Members {
			if _, ok := teams[member.BotID]; !ok {
				teams[member.BotID] = name
			}
		}
	}
	a.teams = teams
}

// terminal reports whether a task's status is an end.
func terminal(status string) bool {
	switch domain.TaskStatus(status) {
	case domain.TaskCompleted, domain.TaskFailed, domain.TaskExpired, domain.TaskCancelled:
		return true
	}
	return false
}

// Audit audits the task the id names when it has ended and was not
// audited, then publishes what is due. A task created before the cursor
// was audited, or ended before the adapter's history starts.
func (a *Auditor) Audit(ctx context.Context, id string) error {
	if a.config.Store.Audited(id) {
		return nil
	}
	task, err := a.config.Source.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if !terminal(task.Status) || task.CreatedAt.Before(a.config.Store.Cursor()) {
		return nil
	}
	if err := a.audit(ctx, task); err != nil {
		return err
	}
	return a.Flush(ctx)
}

// audit reads the task's executions and timeline and counts its
// failures, then saves the store.
func (a *Auditor) audit(ctx context.Context, task domain.TaskSnapshot) error {
	executions, err := a.config.Source.ListExecutions(ctx, task.ID)
	if err != nil {
		return err
	}
	timeline, err := a.config.Source.ListTaskEvents(ctx, application.TaskEventsQuery{ID: domain.TaskID(task.ID), Limit: application.MaxEventLimit})
	if err != nil {
		return err
	}
	team := a.teams[task.BotID]
	for _, f := range Extract(task, executions, timeline, team) {
		if a.ignored(f) {
			continue
		}
		a.config.Store.Observe(f)
	}
	a.config.Store.MarkAudited(task.ID, task.CreatedAt)
	return a.config.Store.Save()
}

// Start sets where the audit's history starts. A store with a cursor
// goes on where the last run stopped, and since is ignored: a task is
// counted once. A store without one starts at since, replaying the tasks
// created from it; without since it starts now: the tasks that ended are
// marked audited unread, and the ones still running are audited when
// they end.
func (a *Auditor) Start(ctx context.Context, since time.Time) error {
	if cursor := a.config.Store.Cursor(); !cursor.IsZero() {
		if !since.IsZero() {
			a.config.Log.Printf("activity-audit: --since ignored: the seen-store goes on from %s", stamp(cursor))
		}
		return a.Reconcile(ctx)
	}
	if !since.IsZero() {
		a.config.Store.SetCursor(since)
		return a.Reconcile(ctx)
	}
	tasks, err := a.config.Source.ListTasks(ctx, application.TaskQuery{Limit: application.MaxTaskLimit})
	if err != nil {
		return err
	}
	cursor := a.config.Now()
	for _, task := range tasks {
		if terminal(task.Status) {
			a.config.Store.MarkAudited(task.ID, task.CreatedAt)
		} else if task.CreatedAt.Before(cursor) {
			cursor = task.CreatedAt
		}
	}
	a.config.Store.SetCursor(cursor)
	return a.config.Store.Save()
}

// Reconcile audits every task created from the cursor that has ended and
// was not audited, oldest first, then moves the cursor to the oldest
// task still unfinished (the newest listed when none is). It is what
// makes a missed wake-up harmless: a gap in a team's timeline, a daemon
// that restarted, a bot in no team. The control plane lists at most the
// newest application.MaxTaskLimit tasks.
func (a *Auditor) Reconcile(ctx context.Context) error {
	cursor := a.config.Store.Cursor()
	tasks, err := a.config.Source.ListTasks(ctx, application.TaskQuery{Since: cursor, Limit: application.MaxTaskLimit})
	if err != nil {
		return err
	}
	if len(tasks) == application.MaxTaskLimit {
		a.config.Log.Printf("activity-audit: %d tasks since %s, the most the control plane lists: older ones are not audited", len(tasks), stamp(cursor))
	}
	slices.Reverse(tasks)
	var unfinished time.Time
	newest := cursor
	for _, task := range tasks {
		if task.CreatedAt.After(newest) {
			newest = task.CreatedAt
		}
		if !terminal(task.Status) {
			if unfinished.IsZero() || task.CreatedAt.Before(unfinished) {
				unfinished = task.CreatedAt
			}
			continue
		}
		if a.config.Store.Audited(task.ID) {
			continue
		}
		if err := a.audit(ctx, task); err != nil {
			return err
		}
	}
	if unfinished.IsZero() {
		unfinished = newest
	}
	a.config.Store.SetCursor(unfinished)
	if err := a.config.Store.Save(); err != nil {
		return err
	}
	return a.Flush(ctx)
}

// ignored reports whether the failure is one the noise rules drop.
func (a *Auditor) ignored(f Failure) bool {
	if f.Tool != toolLocalExec {
		return false
	}
	if f.Program != "" {
		return slices.Contains(a.config.IgnorePrograms, f.Program)
	}
	return a.config.IgnoreTeamPartial && f.Team == a.config.Team
}

// Flush publishes every signature due, oldest first, saving the store
// after each. A publish that fails leaves the rest due for the next
// flush.
func (a *Auditor) Flush(ctx context.Context) error {
	for _, signature := range a.config.Store.Pending() {
		if signature.Due.Count == 0 {
			// What the event says is fixed, and kept, before it is sent.
			signature.Due = DuePublish{Bot: signature.LastBot, Team: a.teams[signature.LastBot], Task: signature.LastTask, Count: signature.Count}
			if err := a.config.Store.Save(); err != nil {
				return err
			}
		}
		eventID := DryRunEventID
		if a.config.Publisher != nil {
			if err := a.pace(ctx); err != nil {
				return err
			}
			answer, err := a.config.Publisher.PublishGroupEvent(ctx, a.command(signature))
			if err != nil {
				return fmt.Errorf("publish %s: %w", signature.ID, err)
			}
			eventID = answer.EventID
		}
		a.config.Store.Published(signature, eventID, a.config.Now())
		verb := "published"
		if a.config.Publisher == nil {
			verb = "would be published (dry run)"
		}
		a.config.Log.Printf("activity-audit: %s %s %s to team %s as %s (%d seen)", a.config.Event, signature.ID, verb, a.config.Team, eventID, signature.Due.Count)
		if err := a.config.Store.Save(); err != nil {
			return err
		}
	}
	return nil
}

// pace waits until one more publish stays within PerMinute in the last
// minute, and counts it.
func (a *Auditor) pace(ctx context.Context) error {
	if a.config.PerMinute == 0 {
		return nil
	}
	for {
		now := a.config.Now()
		kept := a.sent[:0]
		for _, at := range a.sent {
			if now.Sub(at) < time.Minute {
				kept = append(kept, at)
			}
		}
		a.sent = kept
		if len(a.sent) < a.config.PerMinute {
			a.sent = append(a.sent, now)
			return nil
		}
		wait := time.Minute - now.Sub(a.sent[0])
		a.config.Log.Printf("activity-audit: %d events in the last minute, the most: the next in %s", len(a.sent), wait.Round(time.Second))
		if err := a.config.Sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// sleep waits d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// command is the publish of a signature, its data the spec's FailureData,
// from what was fixed when it fell due.
func (a *Auditor) command(signature *SeenSignature) application.PublishGroupEventCommand {
	due := signature.Due
	if due.Bot == "" && len(signature.Bots) > 0 {
		due.Bot = signature.Bots[0]
	}
	if due.Task == "" {
		due.Task = signature.FirstTask
	}
	if due.Team == "" {
		due.Team = a.teams[due.Bot]
	}
	bot, task := due.Bot, due.Task
	data := FailureData{ID: signature.ID, Kind: signature.Kind, Bot: bot, Team: due.Team, Task: task, Tool: signature.Tool, Program: signature.Program, McpTool: signature.McpTool, Error: signature.Error, Partial: signature.Partial, Count: due.Count, FirstAt: signature.FirstAt}
	return application.PublishGroupEventCommand{
		Group:          a.config.Team,
		Name:           a.config.Event,
		Message:        Message(signature, bot, due.Team),
		Data:           dataOf(data),
		IdempotencyKey: IdempotencyKey(signature),
	}
}

// Message is the event's one line, self-sufficient (a coalesced note or
// a redelivery carries only it and data.id):
// "<id> · <tool> · <bot> (<team>) · <program or -> · <error>", cut to
// 280 characters. A failed task's tool is "task"; a bridged call's
// program is the plugin's tool it reached.
func Message(signature *SeenSignature, bot, team string) string {
	tool := signature.Tool
	if signature.Kind == KindTaskFailed {
		tool = "task"
	}
	program := signature.Program
	if program == "" {
		program = signature.McpTool
	}
	line := strings.Join([]string{signature.ID, orDash(tool), orDash(bot) + " (" + orDash(team) + ")", orDash(program), signature.Error}, " · ")
	return cutRunes(strings.Join(strings.Fields(line), " "), domain.MaxEventMessageLength)
}

// orDash is the text, or - when it is empty.
func orDash(text string) string {
	if text == "" {
		return "-"
	}
	return text
}

// dataOf is the generated DTO as the command's data: its keys are the
// spec's, never written here.
func dataOf(data FailureData) map[string]any {
	raw, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var fields map[string]any
	_ = dec.Decode(&fields)
	return fields
}
