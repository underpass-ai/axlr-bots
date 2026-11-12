package activityaudit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Defaults of a watcher: how long one long poll of a team's timeline
// waits (the control plane answers at most 30 seconds), and how long it
// waits before it asks again after the control plane did not answer.
const (
	DefaultWait  = 30 * time.Second
	DefaultRetry = 2 * time.Second
)

// endTypes are the events of a task's end.
var endTypes = map[domain.EventType]bool{
	domain.EventTaskCompleted: true,
	domain.EventTaskFailed:    true,
	domain.EventTaskExpired:   true,
	domain.EventTaskCancelled: true,
}

// WatcherConfig is what a Watcher needs. Once stops after the start's
// reconciliation: a replay of --since, or a dry run.
type WatcherConfig struct {
	Source  Source
	Auditor *Auditor
	Wait    time.Duration
	Retry   time.Duration
	Once    bool
	Log     *log.Logger
}

// Watcher wakes on the teams' timelines and audits each task when it
// ends: event driven, no timer. A team's timeline is only the wake-up;
// what is audited comes from the task, and a gap in a timeline (a window
// overrun, a control plane that went away) is made good by a
// reconciliation over the task list.
type Watcher struct {
	config WatcherConfig
}

// wake is what a team's long poll brought: its events, whether events
// may have been missed before them, or that the team is gone.
type wake struct {
	team   string
	events []domain.Event
	gap    bool
	gone   bool
}

// NewWatcher checks the configuration.
func NewWatcher(config WatcherConfig) (*Watcher, error) {
	if config.Source == nil || config.Auditor == nil {
		return nil, errors.New("activityaudit: a watcher needs a source and an auditor")
	}
	if config.Wait <= 0 {
		config.Wait = DefaultWait
	}
	if config.Retry <= 0 {
		config.Retry = DefaultRetry
	}
	if config.Log == nil {
		config.Log = log.New(io.Discard, "", 0)
	}
	return &Watcher{config: config}, nil
}

// Run starts the audit at since (Auditor.Start), then follows every team
// until ctx ends.
func (w *Watcher) Run(ctx context.Context, since time.Time) error {
	groups, err := w.config.Source.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("list the teams: %w", err)
	}
	w.config.Auditor.SetTeams(groups)
	if err := w.config.Auditor.Start(ctx, since); err != nil {
		return err
	}
	if w.config.Once {
		return nil
	}
	wakes := make(chan wake)
	following := map[string]bool{}
	follow := func(groups []application.Group) {
		for _, group := range groups {
			if !following[group.Name] {
				following[group.Name] = true
				go w.follow(ctx, group, wakes)
			}
		}
	}
	follow(groups)
	w.config.Log.Printf("activity-audit: following %d teams", len(following))
	for {
		select {
		case <-ctx.Done():
			return nil
		case woken := <-wakes:
			if woken.gone {
				delete(following, woken.team)
				continue
			}
			ended := endedTasks(woken.events)
			for _, id := range ended {
				if err := w.config.Auditor.Audit(ctx, id); err != nil && ctx.Err() == nil {
					w.config.Log.Printf("activity-audit: task %s: %v", id, err)
				}
			}
			if woken.gap {
				if err := w.config.Auditor.Reconcile(ctx); err != nil && ctx.Err() == nil {
					w.config.Log.Printf("activity-audit: reconcile: %v", err)
				}
			}
			if len(ended) > 0 || woken.gap {
				// A task's end is when a new team may have formed.
				if groups, err := w.config.Source.ListGroups(ctx); err == nil {
					w.config.Auditor.SetTeams(groups)
					follow(groups)
				}
			}
		}
	}
}

// follow long-polls the team's timeline from its newest event and hands
// each batch over; it ends with ctx, or when the team is gone.
func (w *Watcher) follow(ctx context.Context, group application.Group, wakes chan<- wake) {
	query := application.GroupEventsQuery{Name: group.Name, Tenant: group.TenantID, Limit: 1}
	gap := false
	first := true
	for ctx.Err() == nil {
		wait := w.config.Wait
		if first {
			wait = 0
		}
		events, err := w.config.Source.WaitGroupEvents(ctx, query, wait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.As(err, new(domain.NotFoundError)) {
				w.send(ctx, wakes, wake{team: group.Name, gone: true})
				return
			}
			w.config.Log.Printf("activity-audit: team %s: %v", group.Name, err)
			gap = !first
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.config.Retry):
			}
			continue
		}
		if len(events) > 0 {
			query.After = events[len(events)-1].ID.String()
		}
		full := !first && len(events) >= query.Limit
		if !first && (len(events) > 0 || gap || full) {
			w.send(ctx, wakes, wake{team: group.Name, events: events, gap: gap || full})
		}
		first, gap = false, false
		query.Limit = application.MaxGroupEvents
	}
}

// send hands the wake over unless ctx ends first.
func (w *Watcher) send(ctx context.Context, wakes chan<- wake, woken wake) {
	select {
	case wakes <- woken:
	case <-ctx.Done():
	}
}

// endedTasks are the tasks whose end the events tell, each once, in
// order.
func endedTasks(events []domain.Event) []string {
	var ids []string
	seen := map[string]bool{}
	for _, event := range events {
		id := event.TaskID.String()
		if !endTypes[event.Type] || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
