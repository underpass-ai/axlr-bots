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
	execution domain.ExecutionID
	by        domain.Publisher
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
	before, seen, count := emittedBefore(recent, e.id, uc.deps.Clock.Now())
	if seen {
		// The same call again (a retry): what it delivered then, nothing
		// new; a note or a message is not given twice.
		return repeatedAnswer(e, before), nil
	}
	if count >= e.bot.Events().PerMinute() {
		return uc.refuse(ctx, e, domain.RefusedRateLimited, fmt.Sprintf("bot %s emitted %d events in the last minute, its most; wait before the next", e.bot.ID(), count))
	}
	answer := EmitAnswer{EventID: e.id, Name: e.name.String(), Severity: e.severity}
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
	// The kindless recall is stop.recall, for one release.
	name = name.Canonical()
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
	e := emission{node: node, execution: execution.ID(), by: domain.PublisherBot, task: task, bot: bot, name: name, severity: domain.KindOf(name).Severity(), message: message, data: data, dataJSON: dataJSON, to: to}
	e.id = eventID(task.ID(), e)
	return e, nil
}

// MaxEventTargets is the most teammates one event may be addressed to.
const MaxEventTargets = 16

// deliver gives the teammate the event in the mode its subscription
// says (by the severity when it names none): a task, a message into its
// running task, or a note for its next prompt; or says why it gets
// none; neither when the teammate simply does not hear the event. An
// urgent event is always a task.
func (uc EmitEvent) deliver(ctx context.Context, e emission, mate domain.Bot) (*EventDelivery, *EventSkip, error) {
	addressed := slices.Contains(e.to, mate.ID())
	if len(e.to) > 0 && !addressed {
		return nil, nil, nil
	}
	sub, hears := mate.Hears(e.name, e.bot.ID().String(), e.by, addressed)
	if !hears {
		return nil, nil, nil
	}
	urgent := mate.InterruptedBy(sub, domain.KindOf(e.name))
	mode := sub.ModeFor(e.severity)
	if urgent {
		mode = domain.ModeTask
	}
	switch mode {
	case domain.ModeNote:
		return uc.deliverNote(ctx, e, mate, sub, "", "")
	case domain.ModeMessage:
		if delivery, skip, handled, err := uc.deliverMessage(ctx, e, mate, sub); handled || err != nil {
			return delivery, skip, err
		}
	}
	return uc.deliverTask(ctx, e, mate, sub, urgent)
}

// skipped says why the teammate gets nothing.
func skipped(mate domain.Bot, reason, message string) (*EventDelivery, *EventSkip, error) {
	return nil, &EventSkip{BotID: mate.ID().String(), Reason: reason, Message: message}, nil
}

// recordHeard records on the receiver's stream that it heard the event
// and what the event did to it.
func (uc EmitEvent) recordHeard(ctx context.Context, e emission, mate domain.Bot, d EventDelivery) error {
	payload := map[string]any{
		"eventId": e.id, "name": e.name.String(), "from": e.bot.ID().String(), "by": string(e.by),
		"severity": e.severity.String(), "message": e.message.String(), "mode": d.Mode, "subscription": d.Trigger,
	}
	if d.TaskID != "" {
		payload["taskId"], payload["urgent"] = d.TaskID, d.Urgent
	}
	if d.MessageID != "" {
		payload["messageId"] = d.MessageID
	}
	if d.Degraded != "" {
		payload["degraded"] = d.Degraded
	}
	if d.CoalescedInto != "" {
		payload["coalescedInto"] = d.CoalescedInto
	}
	return uc.rec.event(ctx, domain.EventEventHeard, mate, domain.TaskID(d.TaskID), e.task.CorrelationID(), payload)
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
	if e.by != domain.PublisherBot {
		payload["by"] = string(e.by)
	}
	if err := uc.rec.executionEvent(ctx, domain.EventEventEmitted, e.bot, e.task.ID(), e.execution, e.task.CorrelationID(), payload); err != nil {
		return err
	}
	details := map[string]string{"node": e.node.String(), "by": string(e.by), "execution": e.execution.String(), "event": e.id, "name": e.name.String(), "severity": e.severity.String(), "deliveries": strconv.Itoa(len(answer.Deliveries))}
	return uc.rec.audit(ctx, "event.emit", e.task.ID().String(), domain.AuditOK, details)
}

// refuse records the refusal and answers it.
func (uc EmitEvent) refuse(ctx context.Context, e emission, reason, message string) (EmitAnswer, error) {
	payload := map[string]any{"name": e.name.String(), "severity": e.severity.String(), "message": e.message.String(), "reason": reason, "refusal": message}
	if err := uc.rec.executionEvent(ctx, domain.EventEventRefused, e.bot, e.task.ID(), e.execution, e.task.CorrelationID(), payload); err != nil {
		return EmitAnswer{}, err
	}
	details := map[string]string{"node": e.node.String(), "execution": e.execution.String(), "name": e.name.String(), "reason": reason}
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

// emittedBefore reads the bot's newest events: the record of this event
// when it was emitted already, and how many distinct events the bot
// emitted in the last minute.
func emittedBefore(events []domain.Event, id string, now time.Time) (domain.Event, bool, int) {
	var before domain.Event
	seen := false
	inWindow := map[string]bool{}
	for _, event := range events {
		if event.Type != domain.EventEventEmitted {
			continue
		}
		eventID, _ := event.Payload["eventId"].(string)
		if eventID == id {
			before, seen = event, true
		}
		// What the control plane published about the bot is not the bot's
		// own emitting.
		if by, _ := event.Payload["by"].(string); by == string(domain.PublisherControlPlane) {
			continue
		}
		if now.Sub(event.CreatedAt) < eventRateWindow {
			inWindow[eventID] = true
		}
	}
	return before, seen, len(inWindow)
}

// repeatedAnswer answers a repeat with the deliveries and skips the
// event's record names, none of them created now.
func repeatedAnswer(e emission, before domain.Event) EmitAnswer {
	answer := EmitAnswer{EventID: e.id, Name: e.name.String(), Severity: e.severity, Deduplicated: true}
	// The record is read back from memory or from the event log: through
	// JSON, it reads the same either way.
	if raw, err := json.Marshal(before.Payload["deliveries"]); err == nil {
		_ = json.Unmarshal(raw, &answer.Deliveries)
	}
	if raw, err := json.Marshal(before.Payload["skipped"]); err == nil {
		_ = json.Unmarshal(raw, &answer.Skipped)
	}
	for i := range answer.Deliveries {
		answer.Deliveries[i].Created = false
	}
	return answer
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
		item := map[string]any{"botId": d.BotID, "mode": d.Mode, "trigger": d.Trigger, "created": d.Created, "urgent": d.Urgent}
		if d.TaskID != "" {
			item["taskId"] = d.TaskID
		}
		if d.MessageID != "" {
			item["messageId"] = d.MessageID
		}
		if d.Degraded != "" {
			item["degraded"] = d.Degraded
		}
		if d.CoalescedInto != "" {
			item["coalescedInto"] = d.CoalescedInto
		}
		out = append(out, item)
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
