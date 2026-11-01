package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// PublishGroupEventCommand is what the operator publishes on a team:
// the team (and its tenant when several have one of that name), the
// event's name, its one-line message, the facts, whom it is addressed
// to, and the key a retry repeats.
type PublishGroupEventCommand struct {
	Group          string         `json:"-"`
	Tenant         string         `json:"-"`
	Name           string         `json:"name"`
	Message        string         `json:"message"`
	Data           map[string]any `json:"data,omitempty"`
	To             []string       `json:"to,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
}

// PublishAnswer is what the control plane says to the operator's event:
// its id, who heard it and how, who was skipped and why, and false in
// Created when the key named an event published before.
type PublishAnswer struct {
	EventID    string          `json:"eventId"`
	Name       string          `json:"name"`
	Severity   domain.Severity `json:"severity"`
	Deliveries []EventDelivery `json:"deliveries"`
	Skipped    []EventSkip     `json:"skipped"`
	Created    bool            `json:"created"`
}

// PublishGroupEvent is the operator telling a team that something
// happened: the same checks and deliveries as a bot's event, as the
// operator, less the rate and the chain (its tasks start none). error.*
// and done.* are the control plane's own. stop.all is the generalised
// Recall: every member hears it as an urgent task.
type PublishGroupEvent struct {
	deps Dependencies
	rec  recorder
}

// NewPublishGroupEvent wires the use case.
func NewPublishGroupEvent(deps Dependencies) PublishGroupEvent {
	return PublishGroupEvent{deps: deps, rec: recorder{deps: deps}}
}

// Execute publishes the event, or finds the one the key published.
func (uc PublishGroupEvent) Execute(ctx context.Context, command PublishGroupEventCommand) (PublishAnswer, error) {
	group, err := NewGetGroup(uc.deps).Execute(ctx, command.Group, command.Tenant)
	if err != nil {
		return PublishAnswer{}, err
	}
	team, err := domain.NewTeamName(group.Name)
	if err != nil {
		return PublishAnswer{}, err
	}
	key, err := domain.NewIdempotencyKey(command.IdempotencyKey)
	if err != nil {
		return PublishAnswer{}, err
	}
	name, err := domain.NewEventName(command.Name)
	if err != nil {
		return PublishAnswer{}, err
	}
	if kind := domain.KindOf(name); !kind.PublishedBy(domain.PublisherOperator) {
		return PublishAnswer{}, domain.ValidationError{Field: "name", Reason: domain.RefusedNotAllowed + ": " + kind.String() + ".* is the control plane's own"}
	}
	message, err := domain.NewEventMessage(command.Message)
	if err != nil {
		return PublishAnswer{}, err
	}
	var raw json.RawMessage
	if command.Data != nil {
		if raw, err = json.Marshal(command.Data); err != nil {
			return PublishAnswer{}, domain.ValidationError{Field: "data", Reason: "must be a JSON object"}
		}
	}
	data, dataJSON, err := eventData(raw)
	if err != nil {
		return PublishAnswer{}, err
	}
	members := make([]domain.Bot, 0, len(group.Members))
	for _, member := range group.Members {
		bot, err := uc.deps.Bots.Get(ctx, domain.BotID(member.BotID))
		if err != nil {
			return PublishAnswer{}, err
		}
		members = append(members, bot)
	}
	if len(command.To) > MaxEventTargets {
		return PublishAnswer{}, domain.ValidationError{Field: "to", Reason: "names at most 16 members"}
	}
	var to []domain.BotID
	for _, raw := range command.To {
		id, err := domain.NewBotID(raw)
		if err != nil {
			return PublishAnswer{}, err
		}
		if !slices.ContainsFunc(members, func(m domain.Bot) bool { return m.ID() == id }) {
			return PublishAnswer{}, domain.ValidationError{Field: "to", Reason: fmt.Sprintf("%s: bot %s is not in team %s; its members are: %s", domain.RefusedNotMember, id, group.Name, joinNames(botIDs(members)))}
		}
		if !slices.Contains(to, id) {
			to = append(to, id)
		}
	}
	slices.Sort(to)
	e := emission{by: domain.PublisherOperator, from: domain.FromOperator, teams: []string{group.Name}, name: name, severity: domain.KindOf(name).Severity(), message: message, data: data, dataJSON: dataJSON, to: to}
	e.id = publishedEventID(key, e)
	tenant, err := domain.NewTenantID(group.TenantID)
	if err != nil {
		return PublishAnswer{}, err
	}
	if before, found, err := uc.published(ctx, tenant, team, e.id); err != nil || found {
		return before, err
	}
	answer := PublishAnswer{EventID: e.id, Name: name.String(), Severity: e.severity, Deliveries: []EventDelivery{}, Skipped: []EventSkip{}, Created: true}
	emit := NewEmitEvent(uc.deps)
	for _, member := range members {
		delivery, skip, err := emit.deliver(ctx, e, member)
		if err != nil {
			return PublishAnswer{}, err
		}
		if delivery != nil {
			answer.Deliveries = append(answer.Deliveries, *delivery)
		}
		if skip != nil {
			answer.Skipped = append(answer.Skipped, *skip)
		}
	}
	payload := e.fields()
	delete(payload, "dataJSON")
	delete(payload, "taskId")
	payload["tenant"], payload["group"], payload["by"] = group.TenantID, group.Name, string(domain.PublisherOperator)
	payload["deliveries"], payload["skipped"] = deliveriesPayload(answer.Deliveries), skipsPayload(answer.Skipped)
	if err := uc.rec.groupEvent(ctx, domain.EventGroupEventPublished, tenant, team, payload); err != nil {
		return PublishAnswer{}, err
	}
	details := map[string]string{"group": group.Name, "tenant": group.TenantID, "event": e.id, "name": name.String(), "deliveries": fmt.Sprint(len(answer.Deliveries))}
	return answer, uc.rec.audit(ctx, "group.publish", group.Name, domain.AuditOK, details)
}

// published finds the event the key published on the team before.
func (uc PublishGroupEvent) published(ctx context.Context, tenant domain.TenantID, team domain.TeamName, id string) (PublishAnswer, bool, error) {
	events, err := uc.deps.EventReader.ListTeamEvents(ctx, tenant, team, nil, MaxGroupEvents)
	if err != nil {
		return PublishAnswer{}, false, err
	}
	for _, event := range events {
		if event.Type != domain.EventGroupEventPublished || event.Payload["eventId"] != id {
			continue
		}
		answer := PublishAnswer{EventID: id, Deliveries: []EventDelivery{}, Skipped: []EventSkip{}}
		answer.Name, _ = event.Payload["name"].(string)
		severity, _ := event.Payload["severity"].(string)
		answer.Severity = domain.Severity(severity)
		if raw, err := json.Marshal(event.Payload["deliveries"]); err == nil {
			_ = json.Unmarshal(raw, &answer.Deliveries)
		}
		if raw, err := json.Marshal(event.Payload["skipped"]); err == nil {
			_ = json.Unmarshal(raw, &answer.Skipped)
		}
		for i := range answer.Deliveries {
			answer.Deliveries[i].Created = false
		}
		return answer, true, nil
	}
	return PublishAnswer{}, false, nil
}

// publishedEventID names the operator's event by its key and what it
// says: a retry with the same key is the same event.
func publishedEventID(key domain.IdempotencyKey, e emission) string {
	to := make([]string, len(e.to))
	for i, id := range e.to {
		to[i] = id.String()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{key.String(), e.name.String(), e.message.String(), e.dataJSON, strings.Join(to, ",")}, "\x00")))
	return "evt-" + hex.EncodeToString(sum[:12])
}
