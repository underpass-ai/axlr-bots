package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// eventRateWindow is the span the emit rate is counted over.
const eventRateWindow = time.Minute

// eventHistory is how many of the bot's newest events the rate and the
// repeat check read.
const eventHistory = 500

// EmitEvent is a running execution telling its bot's teams that
// something happened, through its node. The control plane derives the
// emitter from the execution the node holds, checks that the bot is in a
// team, may emit that name (whose kind fixes the severity), addresses only teammates
// and is under its rate, and then makes a task for every teammate that
// hears the event, under the emitter's trace id and lineage, so depth
// and the chain cut a loop as they cut a delegation's. Each receiver
// runs on its own budgets, until its own timeout. The event is recorded
// on the emitter's stream whoever got it.
type EmitEvent struct {
	deps Dependencies
	rec  recorder
}

// NewEmitEvent wires the use case.
func NewEmitEvent(deps Dependencies) EmitEvent {
	return EmitEvent{deps: deps, rec: recorder{deps: deps}}
}

// emission is one event being checked and delivered.
type emission struct {
	node      domain.NodeID
	execution domain.Execution
	task      domain.Task
	bot       domain.Bot
	id        string
	name      domain.EventName
	severity  domain.Severity
	message   domain.EventMessage
	data      map[string]any
	dataJSON  string
	to        []domain.BotID
}

// Execute delivers the event, finds the delivery the same call made, or
// answers why not.
func (uc EmitEvent) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command EmitEventCommand) (EmitAnswer, error) {
	e, err := uc.load(ctx, node, executionID, command)
	if err != nil {
		return EmitAnswer{}, err
	}
	if e.bot.Teams().Empty() {
		return uc.refuse(ctx, e, domain.RefusedNoTeam, "bot "+e.bot.ID().String()+" is in no team: there is nobody to tell")
	}
	if kind := domain.KindOf(e.name); !kind.PublishedBy(domain.PublisherBot) {
		return uc.refuse(ctx, e, domain.RefusedNotAllowed, fmt.Sprintf("%s.* is the control plane's own: a bot may not emit %s", kind, e.name))
	}
	if !e.bot.Events().MayEmit(e.name) {
		return uc.refuse(ctx, e, domain.RefusedNotAllowed, fmt.Sprintf("bot %s may not emit %s; it may emit: %s", e.bot.ID(), e.name, joinNames(emitNames(e.bot))))
	}
	mates, err := teammatesOf(ctx, uc.deps, e.bot)
	if err != nil {
		return EmitAnswer{}, err
	}
	for _, id := range e.to {
		if !slices.ContainsFunc(mates, func(m domain.Bot) bool { return m.ID() == id }) {
			return uc.refuse(ctx, e, domain.RefusedNotMember, fmt.Sprintf("bot %s is not a teammate of %s; its teammates are: %s", id, e.bot.ID(), joinNames(botIDs(mates))))
		}
	}
	recent, err := uc.deps.EventReader.ListEvents(ctx, e.bot.ID(), eventHistory)
	if err != nil {
		return EmitAnswer{}, err
	}
	seen, count := emittedBefore(recent, e.id, uc.deps.Clock.Now())
	if !seen && count >= e.bot.Events().PerMinute() {
		return uc.refuse(ctx, e, domain.RefusedRateLimited, fmt.Sprintf("bot %s emitted %d events in the last minute, its most; wait before the next", e.bot.ID(), count))
	}
	answer := EmitAnswer{EventID: e.id, Name: e.name.String(), Severity: e.severity, Deduplicated: seen}
	for _, mate := range mates {
		delivery, skip, err := uc.deliver(ctx, e, mate)
		if err != nil {
			return EmitAnswer{}, err
		}
		if delivery != nil {
			answer.Deliveries = append(answer.Deliveries, *delivery)
		}
		if skip != nil {
			answer.Skipped = append(answer.Skipped, *skip)
		}
	}
	if seen {
		return answer, nil
	}
	if err := uc.recordEmitted(ctx, e, answer); err != nil {
		return EmitAnswer{}, err
	}
	return answer, nil
}

// load finds the execution the node holds, the task and bot behind it,
// and validates the call.
func (uc EmitEvent) load(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command EmitEventCommand) (emission, error) {
	execution, err := runningExecutionOf(ctx, uc.deps, node, executionID, "emit an event")
	if err != nil {
		return emission{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return emission{}, err
	}
	if task.Status() != domain.TaskRunning {
		return emission{}, domain.StateError{Subject: "task " + task.ID().String(), Current: task.Status().String(), Attempted: "emit an event"}
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return emission{}, err
	}
	name, err := domain.NewEventName(command.Name)
	if err != nil {
		return emission{}, err
	}
	message, err := domain.NewEventMessage(command.Message)
	if err != nil {
		return emission{}, err
	}
	data, dataJSON, err := eventData(command.Data)
	if err != nil {
		return emission{}, err
	}
	if len(command.To) > MaxEventTargets {
		return emission{}, domain.ValidationError{Field: "to", Reason: "names at most 16 teammates"}
	}
	var to []domain.BotID
	for _, raw := range command.To {
		id, err := domain.NewBotID(raw)
		if err != nil {
			return emission{}, err
		}
		if !slices.Contains(to, id) {
			to = append(to, id)
		}
	}
	slices.Sort(to)
	// The kind of the name fixes the severity; an older node's severity
	// is ignored.
	e := emission{node: node, execution: execution, task: task, bot: bot, name: name, severity: domain.KindOf(name).Severity(), message: message, data: data, dataJSON: dataJSON, to: to}
	e.id = eventID(task.ID(), e)
	return e, nil
}

// MaxEventTargets is the most teammates one event may be addressed to.
const MaxEventTargets = 16

// deliver makes the teammate's task, or says why it gets none; neither
// when the teammate simply does not hear the event.
func (uc EmitEvent) deliver(ctx context.Context, e emission, mate domain.Bot) (*EventDelivery, *EventSkip, error) {
	addressed := slices.Contains(e.to, mate.ID())
	if len(e.to) > 0 && !addressed {
		return nil, nil, nil
	}
	skip := func(reason, message string) (*EventDelivery, *EventSkip, error) {
		return nil, &EventSkip{BotID: mate.ID().String(), Reason: reason, Message: message}, nil
	}
	trigger, hears := mate.Hears(e.name, e.severity, addressed)
	switch {
	case !hears && addressed:
		return skip("not_listening", "bot "+mate.ID().String()+" does not listen to "+e.name.String()+": its event triggers do not name it")
	case !hears:
		return nil, nil, nil
	case !mate.AcceptsTasks():
		return skip(domain.RefusedTargetUnavailable, "bot "+mate.ID().String()+" is "+mate.Status().String()+" and takes no task")
	}
	limit := chainDepthLimit(e.bot, e.task)
	depth := e.task.Depth() + 1
	if depth > limit {
		return skip(domain.RefusedDepthExceeded, fmt.Sprintf("a task of %s would be at depth %d; this chain allows %d", mate.ID(), depth, limit))
	}
	lineage, _ := e.task.Lineage()
	if lineage.Visited(mate.ID()) {
		return skip(domain.RefusedCycle, fmt.Sprintf("bot %s is already on this chain (%s): telling it would loop", mate.ID(), strings.Join(append(lineage.Chain, e.bot.ID().String()), " → ")))
	}
	template := trigger.ObjectiveTemplate()
	if template == "" {
		template = domain.DefaultEventObjective
	}
	objective, err := domain.RenderObjective(template, e.fields())
	if err != nil {
		return skip("objective", "the objective of "+mate.ID().String()+"'s trigger "+trigger.Name()+" does not render: "+err.Error())
	}
	key, err := domain.NewIdempotencyKey("event:" + e.id + ":" + mate.ID().String())
	if err != nil {
		return nil, nil, err
	}
	now := uc.deps.Clock.Now()
	child := domain.TaskLineage{
		ParentTaskID: e.task.ID().String(), ParentExecutionID: e.execution.ID().String(), ParentBotID: e.bot.ID().String(),
		Depth: depth, MaxDepth: limit, Chain: append(lineage.Chain, e.bot.ID().String()),
		Grant: domain.DelegationGrant{Deadline: now.Add(mate.Budgets().TaskTimeoutOr(DefaultTaskTimeout))}, EventID: e.id,
	}
	input, err := json.Marshal(e.fields())
	if err != nil {
		return nil, nil, err
	}
	s := submission{
		action: "task.event", bot: mate, trigger: trigger.Ref(), objective: objective,
		key: key, correlation: e.task.CorrelationID(), input: input, lineage: &child,
	}
	if mate.InterruptedBy(trigger, e.severity, addressed) {
		s.priority, s.why = domain.PriorityUrgent, "a "+e.severity.String()+" from its teammate "+e.bot.ID().String()+", event "+e.name.String()
	}
	created, err := submitTask(ctx, uc.deps, s)
	if err != nil {
		return nil, nil, err
	}
	return &EventDelivery{BotID: mate.ID().String(), TaskID: created.Task.ID().String(), Trigger: trigger.Name(), Created: created.Created, Urgent: created.Task.IsUrgent()}, nil, nil
}

// fields are what an event trigger's objective template reads and what
// the receiver's task keeps as its input.
func (e emission) fields() map[string]any {
	to := make([]string, len(e.to))
	for i, id := range e.to {
		to[i] = id.String()
	}
	teams := make([]string, 0)
	for _, team := range e.bot.Teams().Names() {
		teams = append(teams, team.String())
	}
	return map[string]any{
		"eventId": e.id, "name": e.name.String(), "from": e.bot.ID().String(), "severity": e.severity.String(),
		"message": e.message.String(), "data": e.data, "dataJSON": e.dataJSON, "to": to, "teams": teams,
		"taskId": e.task.ID().String(),
	}
}

func (uc EmitEvent) recordEmitted(ctx context.Context, e emission, answer EmitAnswer) error {
	payload := e.fields()
	delete(payload, "dataJSON")
	delete(payload, "taskId")
	payload["deliveries"], payload["skipped"] = deliveriesPayload(answer.Deliveries), skipsPayload(answer.Skipped)
	if err := uc.rec.executionEvent(ctx, domain.EventEventEmitted, e.bot, e.task.ID(), e.execution.ID(), e.task.CorrelationID(), payload); err != nil {
		return err
	}
	details := map[string]string{"node": e.node.String(), "execution": e.execution.ID().String(), "event": e.id, "name": e.name.String(), "severity": e.severity.String(), "deliveries": strconv.Itoa(len(answer.Deliveries))}
	return uc.rec.audit(ctx, "event.emit", e.task.ID().String(), domain.AuditOK, details)
}

// refuse records the refusal and answers it.
func (uc EmitEvent) refuse(ctx context.Context, e emission, reason, message string) (EmitAnswer, error) {
	payload := map[string]any{"name": e.name.String(), "severity": e.severity.String(), "message": e.message.String(), "reason": reason, "refusal": message}
	if err := uc.rec.executionEvent(ctx, domain.EventEventRefused, e.bot, e.task.ID(), e.execution.ID(), e.task.CorrelationID(), payload); err != nil {
		return EmitAnswer{}, err
	}
	details := map[string]string{"node": e.node.String(), "execution": e.execution.ID().String(), "name": e.name.String(), "reason": reason}
	if err := uc.rec.audit(ctx, "event.emit", e.task.ID().String(), domain.AuditDenied, details); err != nil {
		return EmitAnswer{}, err
	}
	return EmitAnswer{Name: e.name.String(), Severity: e.severity, Refused: &domain.EventRefusal{Reason: reason, Message: message}}, nil
}

// eventID names the event by what the task said, so the same call made
// twice (a retry) is the same event and delivers nothing twice.
func eventID(task domain.TaskID, e emission) string {
	to := make([]string, len(e.to))
	for i, id := range e.to {
		to[i] = id.String()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{task.String(), e.name.String(), e.message.String(), e.dataJSON, strings.Join(to, ",")}, "\x00")))
	return "evt-" + hex.EncodeToString(sum[:12])
}

// eventData reads the facts: absent, null or a JSON object of at most
// 8 KiB; it answers them decoded and as compact JSON.
func eventData(raw json.RawMessage) (map[string]any, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, "", nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, "", domain.ValidationError{Field: "data", Reason: "must be a JSON object"}
	}
	if compact.Len() > MaxEventData {
		return nil, "", domain.ValidationError{Field: "data", Reason: "must be at most 8 KiB as JSON"}
	}
	var data map[string]any
	if err := json.Unmarshal(compact.Bytes(), &data); err != nil || data == nil {
		return nil, "", domain.ValidationError{Field: "data", Reason: "must be a JSON object"}
	}
	return data, compact.String(), nil
}

// emittedBefore reads the bot's newest events: whether this event was
// recorded already, and how many distinct events it emitted in the
// last minute.
func emittedBefore(events []domain.Event, id string, now time.Time) (bool, int) {
	seen := false
	inWindow := map[string]bool{}
	for _, event := range events {
		if event.Type != domain.EventEventEmitted {
			continue
		}
		eventID, _ := event.Payload["eventId"].(string)
		seen = seen || eventID == id
		if now.Sub(event.CreatedAt) < eventRateWindow {
			inWindow[eventID] = true
		}
	}
	return seen, len(inWindow)
}

// emitNames lists what the bot may emit, for a refusal's message; never
// error.* or done.*, whatever the rules say.
func emitNames(bot domain.Bot) []string {
	var names []string
	for _, rule := range bot.Events().Rules() {
		names = append(names, rule.Pattern().String())
	}
	return names
}

func botIDs(bots []domain.Bot) []string {
	ids := make([]string, len(bots))
	for i, bot := range bots {
		ids[i] = bot.ID().String()
	}
	return ids
}

func deliveriesPayload(deliveries []EventDelivery) []map[string]any {
	out := make([]map[string]any, 0, len(deliveries))
	for _, d := range deliveries {
		out = append(out, map[string]any{"botId": d.BotID, "taskId": d.TaskID, "trigger": d.Trigger, "created": d.Created, "urgent": d.Urgent})
	}
	return out
}

func skipsPayload(skips []EventSkip) []map[string]any {
	out := make([]map[string]any, 0, len(skips))
	for _, s := range skips {
		out = append(out, map[string]any{"botId": s.BotID, "reason": s.Reason, "message": s.Message})
	}
	return out
}
