package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AlertmanagerSource is the source of an incident an alertmanager
// trigger opened.
const AlertmanagerSource = "alertmanager"

// fireAlertmanager takes Alertmanager's payload on the bot's trigger: a
// firing group opens an incident and its triage task, repeats the one
// the group already has (its repeat_interval resends the same group,
// slightly different each time) or reopens one resolved less than the
// window ago; a resolved group closes the incident and starts nothing;
// the alerts that went green inside a firing group are marked. Every
// fire is a bot.trigger.fired event that says what it did.
func (uc FireTrigger) fireAlertmanager(ctx context.Context, bot domain.Bot, trigger domain.Trigger, body []byte) (FireTriggerResult, error) {
	payload, err := domain.ParseAlertmanagerPayload(body)
	if err != nil {
		_ = uc.rec.audit(ctx, "trigger.fire", bot.ID().String(), domain.AuditDenied, map[string]string{"trigger": trigger.Name(), "reason": err.Error()})
		return FireTriggerResult{}, err
	}
	fired := map[string]any{"trigger": trigger.Name(), "type": trigger.Type().String(), "groupKey": payload.GroupKey, "status": payload.Status, "alerts": len(payload.Alerts)}
	result := FireTriggerResult{Trigger: trigger.Ref()}
	if payload.Firing() {
		opened, err := NewOpenIncident(uc.deps).open(ctx, opening{
			bot: bot, trigger: trigger, source: AlertmanagerSource, groupKey: payload.GroupKey, title: payload.Title(), severity: payload.Severity(),
			fingerprints: payload.FiringFingerprints(), input: body, fields: map[string]any{"alert": payload.Fields()},
		})
		if err != nil {
			return FireTriggerResult{}, err
		}
		incident := opened.Incident
		if resolved := payload.ResolvedFingerprints(); len(resolved) > 0 {
			if err := incident.MarkResolved(resolved, uc.deps.Clock.Now()); err != nil {
				return FireTriggerResult{}, err
			}
			if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
				return FireTriggerResult{}, err
			}
			fired["resolvedAlerts"] = resolved
		}
		result.Incident, result.Task, result.Created, result.Action = &incident, opened.Task, opened.Created, opened.Action
		if opened.Task != nil {
			result.IdempotencyKey = opened.Task.IdempotencyKey()
		}
	} else {
		incident, err := uc.resolveGroup(ctx, bot, trigger, payload)
		if err != nil {
			return FireTriggerResult{}, err
		}
		result.Action = IncidentIgnored
		if incident != nil {
			result.Incident, result.Action = incident, IncidentClosed
		}
	}
	fired["action"], fired["deduplicated"] = result.Action, result.Action == IncidentRepeated
	var task domain.TaskID
	var correlation domain.CorrelationID
	if result.Incident != nil {
		fired["incidentId"] = result.Incident.ID().String()
	}
	if result.Task != nil {
		task, correlation = result.Task.ID(), result.Task.CorrelationID()
		fired["taskId"], fired["idempotencyKey"], fired["created"] = task.String(), result.IdempotencyKey.String(), result.Created
	}
	if err := uc.rec.event(ctx, domain.EventTriggerFired, bot, task, correlation, fired); err != nil {
		return FireTriggerResult{}, err
	}
	return result, nil
}

// resolveGroup closes the incident a resolved group names, when the bot
// has one open; a group nobody had, or one already resolved, changes
// nothing.
func (uc FireTrigger) resolveGroup(ctx context.Context, bot domain.Bot, trigger domain.Trigger, payload domain.AlertmanagerPayload) (*domain.Incident, error) {
	incident, found, err := uc.deps.Incidents.FindNewestByGroupKey(ctx, bot.TenantID(), payload.GroupKey)
	if err != nil {
		return nil, err
	}
	if !found || incident.Status().IsResolved() {
		return nil, uc.rec.audit(ctx, "trigger.fire", bot.ID().String(), domain.AuditOK, map[string]string{"trigger": trigger.Name(), "groupKey": payload.GroupKey, "ignored": "no open incident for the resolved group"})
	}
	now := uc.deps.Clock.Now()
	if err := incident.MarkResolved(payload.ResolvedFingerprints(), now); err != nil {
		return nil, err
	}
	reason := "every alert of the group resolved, said " + trigger.Name()
	if err := incident.Resolve(reason, now); err != nil {
		return nil, err
	}
	if err := uc.deps.Incidents.Save(ctx, incident); err != nil {
		return nil, err
	}
	payloadOut := map[string]any{"by": "trigger:" + trigger.Name(), "reason": reason, "cycle": incident.Cycle(), "costUsd": incident.CostUSD()}
	if err := uc.rec.incidentEvent(ctx, domain.EventIncidentResolved, bot, incident, payloadOut); err != nil {
		return nil, err
	}
	if err := uc.rec.audit(ctx, "incident.resolve", incident.ID().String(), domain.AuditOK, map[string]string{"trigger": trigger.Name(), "reason": reason}); err != nil {
		return nil, err
	}
	return &incident, nil
}
