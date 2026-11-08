package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// incidentOutputQueue queues a delivery per output target that wants an
// incident event, right after the event is published. Queueing is
// idempotent: the delivery id is derived from the event and the target.
// A queueing that fails is audited and leaves the incident's move alone:
// an announcement must never undo or stop what it announces.
type incidentOutputQueue struct {
	deps Dependencies
	rec  recorder
}

// queue writes the deliveries; DeliverOutputs makes the attempts.
func (q incidentOutputQueue) queue(ctx context.Context, event domain.Event, incident domain.Incident) {
	if q.deps.Outputs == nil || q.deps.OutputDeliveries == nil {
		return
	}
	targets, err := q.deps.Outputs.List(ctx)
	if err != nil {
		q.refused(ctx, incident, event, "", err)
		return
	}
	var base *IncidentOutputEvent
	for _, target := range targets {
		if !target.Wants(event.Type, incident) {
			continue
		}
		if base == nil {
			body := q.body(ctx, event, incident)
			base = &body
		}
		if err := q.queueOne(ctx, target, event, incident, *base); err != nil {
			q.refused(ctx, incident, event, target.Name().String(), err)
		}
	}
}

func (q incidentOutputQueue) queueOne(ctx context.Context, target domain.OutputTarget, event domain.Event, incident domain.Incident, base IncidentOutputEvent) error {
	id, err := domain.NewDeliveryID(outputDeliveryID(event.ID, target.Name()))
	if err != nil {
		return err
	}
	if _, err := q.deps.OutputDeliveries.Get(ctx, id); err == nil {
		return nil
	} else if !domain.IsNotFound(err) {
		return err
	}
	body := base
	body.DeliveryID, body.Target = id.String(), target.Name().String()
	body.Links.Panel = target.PanelLink(incident.ID(), q.deps.PanelURL)
	var payload []byte
	if target.Kind() == domain.OutputSlack {
		payload, err = json.Marshal(NewSlackIncidentMessage(body))
	} else {
		payload, err = json.Marshal(body)
	}
	if err != nil {
		return err
	}
	delivery, err := domain.NewOutputDelivery(id, target, incident, event.ID, event.Type, payload, q.deps.Clock.Now())
	if err != nil {
		return err
	}
	if err := q.deps.OutputDeliveries.Save(ctx, delivery); err != nil {
		return err
	}
	return q.rec.audit(ctx, "incident.output", id.String(), domain.AuditOK, outputDetails(delivery, map[string]string{"status": "queued"}))
}

// refused audits a queueing that failed; the audit's own failure has
// nowhere else to go.
func (q incidentOutputQueue) refused(ctx context.Context, incident domain.Incident, event domain.Event, target string, err error) {
	details := map[string]string{"incident": incident.ID().String(), "event": event.Type.String(), "status": "not queued", "reason": err.Error()}
	if target != "" {
		details["target"] = target
	}
	_ = q.rec.audit(ctx, "incident.output", incident.ID().String(), domain.AuditFailed, details)
}

// body is the incident event as every target that wants it receives
// it, before its delivery id, its name and its panel link.
func (q incidentOutputQueue) body(ctx context.Context, event domain.Event, incident domain.Incident) IncidentOutputEvent {
	bot := incident.BotID()
	if current, ok := incident.CurrentTask(); ok && current.BotID() != "" {
		bot = current.BotID()
	}
	botName := bot.String()
	if found, err := q.deps.Bots.Get(ctx, bot); err == nil {
		botName = found.Name().String()
	}
	path := "/v1/incidents/" + url.PathEscape(incident.ID().String())
	body := IncidentOutputEvent{
		Schema: IncidentOutputSchema, Event: event.Type.String(), EventID: event.ID.String(), OccurredAt: event.CreatedAt,
		IncidentID: incident.ID().String(), TenantID: incident.TenantID().String(), BotID: bot.String(), BotName: botName,
		TaskID: incident.CurrentTaskID().String(), Title: incident.Title(), Severity: incident.Severity().String(),
		Status: incident.Status().String(), Cycle: incident.Cycle(), GroupKey: incident.GroupKey(), Source: incident.Source(),
		Reason: incident.Reason(), CostUSD: incident.CostUSD(), CostUnknown: incident.CostUnknown(),
		OpenedAt: incident.OpenedAt(), UpdatedAt: incident.UpdatedAt(),
		NeedsYou: q.needsYou(ctx, incident),
		Links:    IncidentOutputLinks{Incident: ApprovalAction{Method: "GET", Path: path}},
	}
	body.Diagnosis, body.DiagnosisTruncated = outputExcerpt(incident.Diagnosis())
	body.Proposal, body.ProposalTruncated = outputExcerpt(incident.Proposal())
	if base := strings.TrimRight(q.deps.OperatorURL, "/"); base != "" {
		body.Links.Incident.URL = base + path
	}
	if resolvedAt, ok := incident.ResolvedAt(); ok && incident.Status().IsResolved() {
		at := resolvedAt
		body.ResolvedAt = &at
		body.DurationSeconds = int64(resolvedAt.Sub(incident.OpenedAt()).Seconds())
	}
	return body
}

// needsYou is what GET /v1/incidents/{id} answers as needsYou: the
// incident's current task waits for an approval where the incident
// stands. A task that cannot be read counts as not waiting.
func (q incidentOutputQueue) needsYou(ctx context.Context, incident domain.Incident) bool {
	current := incident.CurrentTaskID()
	if current == "" {
		return false
	}
	task, err := q.deps.Tasks.Get(ctx, current)
	return err == nil && incident.NeedsPerson(task.Status() == domain.TaskWaitingApproval)
}

// outputExcerpt cuts a text to MaxOutputExcerpt and says whether it did.
func outputExcerpt(text string) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	excerpt := domain.IncidentText(text, MaxOutputExcerpt)
	return excerpt, len(strings.TrimSpace(text)) > MaxOutputExcerpt
}

// outputDeliveryID is the delivery of one event to one output target:
// the same on every queueing, so neither the outbox nor the receiver
// sees it twice, and never the id of an approval notification.
func outputDeliveryID(event domain.EventID, target domain.OutputName) string {
	sum := sha256.Sum256([]byte("incident.output\x00" + event.String() + "\x00" + target.String()))
	return "dlv-" + hex.EncodeToString(sum[:12])
}

// outputDetails are the audit details of a delivery: the target, the
// incident, the event, never the URL or the secret.
func outputDetails(delivery domain.OutputDelivery, extra map[string]string) map[string]string {
	details := map[string]string{
		"delivery": delivery.ID().String(), "target": delivery.Output().String(), "kind": delivery.Kind().String(),
		"incident": delivery.IncidentID().String(), "event": delivery.Event().String(),
	}
	for key, value := range extra {
		details[key] = value
	}
	return details
}
