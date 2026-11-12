package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// PlatformTaskIncomplete: an event was given again MaxRedeliveries times
// and its final event never came.
const PlatformTaskIncomplete domain.EventName = "warn.incomplete"

// Bounds of what a redelivery and warn.incomplete name.
const (
	// maxIncompleteListed is the most events or tasks warn.incomplete's
	// data and a redelivery's objective list one by one.
	maxIncompleteListed = 16
	// RedeliveryKeyPrefix opens the key of the one task a redelivery
	// makes for a task that ended: redeliver:<task id>.
	RedeliveryKeyPrefix = "redeliver:"
)

// completionKeeper holds a task whose subscription expects final events
// to its contract when it ends (docs/architecture/team-subscriptions.md,
// "Done is a contract"): the carried events without their final event
// are given again in one new task of the same subscription, picked as a
// team delivery picks, at most MaxRedeliveries times each; after that the
// control plane publishes warn.incomplete and audits it. A task that
// expects nothing is none of its business and is never read again.
type completionKeeper struct {
	deps Dependencies
	rec  recorder
}

func newCompletionKeeper(deps Dependencies) completionKeeper {
	return completionKeeper{deps: deps, rec: recorder{deps: deps}}
}

// taskEnded checks a task that completed, failed or expired: each ends
// the work its run did, whether or not it emitted what it owed. A task
// cancelled by a person, by a preemption or by Stop is not checked: whoever
// cancelled it chose to drop its work.
func (k completionKeeper) taskEnded(ctx context.Context, bot domain.Bot, task domain.Task) error {
	switch task.Status() {
	case domain.TaskCompleted, domain.TaskFailed, domain.TaskExpired:
		return k.settle(ctx, bot, task)
	}
	return nil
}

// taskStuck checks a task the team's Jev watcher judged stuck and the
// control plane cancelled: nobody chose to drop its work.
func (k completionKeeper) taskStuck(ctx context.Context, bot domain.Bot, task domain.Task) error {
	if task.Status() != domain.TaskCancelled {
		return nil
	}
	return k.settle(ctx, bot, task)
}

// settle gives again what is missing and warns about what was given
// again its most.
func (k completionKeeper) settle(ctx context.Context, bot domain.Bot, task domain.Task) error {
	missing := task.Missing()
	if len(missing) == 0 {
		return nil
	}
	var again, exhausted []domain.CarriedEvent
	for _, carried := range missing {
		if carried.Redeliveries >= domain.MaxRedeliveries {
			exhausted = append(exhausted, carried)
		} else {
			again = append(again, carried)
		}
	}
	if len(again) > 0 {
		if err := k.redeliver(ctx, bot, task, again); err != nil {
			return err
		}
	}
	if len(exhausted) > 0 {
		return k.incomplete(ctx, bot, task, exhausted)
	}
	return nil
}

// redeliver makes the one task that carries the events again, under the
// key redeliver:<task id>, looked for among every candidate first, so a
// crash between the end and the save never makes two.
func (k completionKeeper) redeliver(ctx context.Context, bot domain.Bot, task domain.Task, again []domain.CarriedEvent) error {
	key, err := domain.NewIdempotencyKey(RedeliveryKeyPrefix + task.ID().String())
	if err != nil {
		return err
	}
	candidates, err := k.candidates(ctx, bot, task)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if _, found, err := k.deps.Tasks.FindByIdempotencyKey(ctx, candidate.ID(), key, candidate.CreatedAt()); err != nil || found {
			return err
		}
	}
	var pool []domain.Bot
	for _, candidate := range candidates {
		if candidate.AcceptsTasks() {
			pool = append(pool, candidate)
		}
	}
	if len(pool) == 0 {
		// Nobody takes tasks now (Paused): the task waits for its bot,
		// as another team's fact does, rather than being lost.
		pool = candidates
	}
	picked, err := pickMember(ctx, k.deps, pool)
	if err != nil {
		return err
	}
	round := 0
	carried := make([]domain.CarriedEvent, len(again))
	for i, c := range again {
		c.Redeliveries++
		c.Tasks = append(c.Tasks, task.ID().String())
		round = max(round, c.Redeliveries)
		carried[i] = c
	}
	memory, team, err := k.contextOf(ctx, bot, task)
	if err != nil {
		return err
	}
	objective, err := redeliveryObjective(picked, task, carried, round, memory)
	if err != nil {
		return err
	}
	input, err := redeliveryInput(task, carried, round, memory, team)
	if err != nil {
		return err
	}
	s := submission{
		action: "task.redeliver", bot: picked, trigger: task.Trigger(), objective: objective, key: key,
		correlation: task.CorrelationID(), input: input, waits: true, expects: task.Expects(), carried: carried,
	}
	if origin, crossed := task.Origin(); crossed {
		s.origin = &origin
	}
	created, err := submitTask(ctx, k.deps, s)
	if err != nil {
		return err
	}
	details := map[string]string{"redelivery": created.Task.ID().String(), "bot": picked.ID().String(), "round": strconv.Itoa(round), "events": strconv.Itoa(len(carried)), "status": task.Status().String()}
	if memory != "" {
		details["context"] = memory
	}
	return k.rec.audit(ctx, "task.redeliver", task.ID().String(), domain.AuditOK, details)
}

// candidates are who may take the events again: the task's bot, and,
// when its subscription gives its work to the team, every member of the
// bot's memory team whose active subscription of the same topic does too,
// by id.
func (k completionKeeper) candidates(ctx context.Context, bot domain.Bot, task domain.Task) ([]domain.Bot, error) {
	out := []domain.Bot{bot}
	sub, ok := bot.Subscriptions().Find(domain.EventPattern(task.Trigger().Name))
	if !ok || !sub.Deliver().Team() {
		return out, nil
	}
	mates, err := teammatesOf(ctx, k.deps, bot)
	if err != nil {
		return nil, err
	}
	team := memoryTeamOf(bot)
	for _, mate := range mates {
		if mate.ID() == bot.ID() || memoryTeamOf(mate) != team || mate.TenantID() != bot.TenantID() {
			continue
		}
		theirs, ok := mate.Subscriptions().Find(sub.Topic())
		if ok && theirs.Active() && theirs.Deliver().Team() && theirs.Team() == sub.Team() {
			out = append(out, mate)
		}
	}
	slices.SortFunc(out, func(a, b domain.Bot) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	return out, nil
}

// contextOf is the KMP key where the ended task's run left what it did,
// and the team whose store holds it: AXLR keeps each turn of a session
// under ws:<session id> in the store the run opened, its bot's memory
// team's. A Jev run keeps none; a task whose run left no session hands
// on the key it was given itself, if any. Empty when none is known.
func (k completionKeeper) contextOf(ctx context.Context, bot domain.Bot, task domain.Task) (string, string, error) {
	team := memoryTeamOf(bot).String()
	if _, jev := bot.Runtime().Jev(); !jev && task.CurrentExecutionID() != "" {
		execution, err := k.deps.Executions.Get(ctx, task.CurrentExecutionID())
		if err != nil && !domain.IsNotFound(err) {
			return "", "", err
		}
		if session := execution.Result().SessionID; err == nil && session != "" {
			if key, err := domain.NewContextKey("ws:" + session); err == nil && team != "" {
				return key.String(), team, nil
			}
		}
	}
	if earlier, ok := redeliveryOf(task); ok && earlier.Context != "" {
		return earlier.Context, earlier.ContextTeam, nil
	}
	return "", "", nil
}

// incomplete publishes warn.incomplete, as the task's bot and by the
// control plane (heard only by a subscription that names it, never by the
// bot), and audits it whoever hears it. Its message and data are the
// same for the same task, so a repeat publishes nothing twice.
func (k completionKeeper) incomplete(ctx context.Context, bot domain.Bot, task domain.Task, exhausted []domain.CarriedEvent) error {
	var events []map[string]any
	var ids []string
	tasks := []string{task.ID().String()}
	for _, c := range exhausted {
		if len(events) < maxIncompleteListed {
			event := map[string]any{"eventId": c.EventID, "name": c.Name}
			if c.ID != "" {
				event["id"] = c.ID
			}
			events = append(events, event)
			ids = append(ids, c.EventID)
		}
		for _, earlier := range c.Tasks {
			if !slices.Contains(tasks, earlier) {
				tasks = append(tasks, earlier)
			}
		}
	}
	slices.Sort(tasks)
	if len(tasks) > maxIncompleteListed {
		tasks = tasks[:maxIncompleteListed]
	}
	subscription := task.Trigger().Name
	message := fmt.Sprintf("task %s of %s (%s) ended %s without the final event for %d event(s), given again %d times: nobody is given them again", task.ID(), bot.ID(), subscription, strings.ToLower(task.Status().String()), len(exhausted), domain.MaxRedeliveries)
	data := map[string]any{"taskId": task.ID().String(), "bot": bot.ID().String(), "subscription": subscription, "events": events, "left": len(exhausted), "tasks": tasks}
	if err := newPlatformPublisher(k.deps).publish(ctx, bot, task, task.CurrentExecutionID(), PlatformTaskIncomplete, message, data); err != nil {
		return err
	}
	return k.rec.audit(ctx, "task.incomplete", task.ID().String(), domain.AuditDenied, map[string]string{
		"bot": bot.ID().String(), "subscription": subscription, "events": strings.Join(ids, ","), "left": strconv.Itoa(len(exhausted)), "tasks": strings.Join(tasks, ","),
	})
}

// TaskRedelivery is what a redelivery task keeps of why it was made, as
// input.redelivery: the round, the task that ended without the finals,
// how it ended, the events it carries again and the KMP key (and the team
// whose store holds it) where the ended task's run left what it did.
type TaskRedelivery struct {
	Round       int                   `json:"round"`
	Of          string                `json:"of"`
	Status      string                `json:"status"`
	Events      []domain.CarriedEvent `json:"events"`
	Context     string                `json:"context,omitempty"`
	ContextTeam string                `json:"contextTeam,omitempty"`
}

// redeliveryOf reads what a redelivery task keeps of why it was made;
// false for any other task.
func redeliveryOf(task domain.Task) (TaskRedelivery, bool) {
	var input struct {
		Redelivery *TaskRedelivery `json:"redelivery"`
	}
	if json.Unmarshal(task.Input(), &input) != nil || input.Redelivery == nil {
		return TaskRedelivery{}, false
	}
	return *input.Redelivery, true
}

// redeliveryInput is the redelivery task's input: the first event it
// carries again as an event task's fields (its name, so a Jev program
// reads it and a later event of the name joins it; data.id and the KMP
// key as data.context), and why it was made.
func redeliveryInput(task domain.Task, carried []domain.CarriedEvent, round int, memory, team string) ([]byte, error) {
	first := carried[0]
	data := map[string]any{}
	if first.ID != "" {
		data["id"] = first.ID
	}
	if memory != "" {
		data["context"] = memory
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"eventId": first.EventID, "name": first.Name, "from": first.From, "message": first.Message,
		"data": data, "dataJSON": string(dataJSON), "taskId": task.ID().String(),
		"redelivery": TaskRedelivery{Round: round, Of: task.ID().String(), Status: task.Status().String(), Events: carried, Context: memory, ContextTeam: team},
	})
}

// redeliveryObjective says what the task owes, which events, where the
// earlier run's memory is, and the earlier objective, cut to what the
// picked bot's prompt has room for.
func redeliveryObjective(picked domain.Bot, task domain.Task, carried []domain.CarriedEvent, round int, memory string) (domain.Objective, error) {
	names := make([]string, 0, len(task.Expects()))
	for _, pattern := range task.Expects() {
		names = append(names, pattern.String())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Redelivery %d of %d: task %s ended %s without its final event for %d event(s) it carried. For each one, emit one event whose name fits %s and whose data.id is that event's id:",
		round, domain.MaxRedeliveries, task.ID(), strings.ToLower(task.Status().String()), len(carried), strings.Join(names, ", "))
	for i, c := range carried {
		if i == maxIncompleteListed {
			fmt.Fprintf(&b, "\n- and %d more", len(carried)-i)
			break
		}
		id := c.ID
		if id == "" {
			id = "(none: any final event settles it)"
		}
		fmt.Fprintf(&b, "\n- %s, data.id %s, from %s: %s", c.Name, id, c.From, strings.Join(strings.Fields(c.Message), " "))
	}
	if memory != "" {
		b.WriteString("\nWhat the earlier task did is in your team's memory under " + memory + ".")
	}
	text := b.String() + "\n\nThe earlier task's objective:\n" + task.Objective().String()
	if room := max(domain.ObjectiveRoomBeside(picked.Mission()), 0); len(text) > room {
		text = strings.ToValidUTF8(text[:room], "")
	}
	return domain.NewObjective(text)
}
