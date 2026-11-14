package topicwire

import (
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Publishers, as the envelope names them.
const (
	PublisherBot          = string(domain.PublisherBot)
	PublisherOperator     = string(domain.PublisherOperator)
	PublisherControlPlane = string(domain.PublisherControlPlane)
)

// TopicsOf maps an event the control plane recorded to one envelope per
// team it was told to: a bot's emitted event (the bot's, or the control
// plane's when it told a fact of its own about the bot) and the
// operator's publication on a team. Any other event is no team's topic,
// and maps to none.
func TopicsOf(event domain.Event) []TopicEventDTO {
	var teams []string
	base := TopicEventDTO{
		SpecVersion: SpecVersion, ID: event.ID.String(), Tenant: event.TenantID.String(),
		TraceID: event.CorrelationID.String(), At: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	switch event.Type {
	case domain.EventEventEmitted:
		teams = stringsOf(event.Payload["teams"])
		base.Publisher = PublisherBot
		if by, _ := event.Payload["by"].(string); by == PublisherControlPlane {
			base.Publisher = PublisherControlPlane
		}
		if base.Bot, _ = event.Payload["from"].(string); base.Bot == "" {
			base.Bot = event.BotID.String()
		}
	case domain.EventGroupEventPublished:
		teams = []string{event.GroupName}
		base.Publisher = PublisherOperator
	default:
		return nil
	}
	base.Name, _ = event.Payload["name"].(string)
	base.Severity, _ = event.Payload["severity"].(string)
	base.Message, _ = event.Payload["message"].(string)
	if data, ok := event.Payload["data"].(map[string]any); ok && len(data) > 0 {
		base.Data = data
	}
	base.To = stringsOf(event.Payload["to"])
	out := make([]TopicEventDTO, 0, len(teams))
	for _, team := range teams {
		if team == "" || base.Name == "" {
			continue
		}
		dto := base
		dto.Team = team
		out = append(out, dto)
	}
	return out
}

// PublicationCommand is the operator's command a publication makes on
// the team and tenant its subject resolved to, with its idempotency key.
func PublicationCommand(p PublicationDTO, team, tenant, key string) application.PublishGroupEventCommand {
	return application.PublishGroupEventCommand{Group: team, Tenant: tenant, Name: p.Name, Message: p.Message, Data: p.Data, To: p.To, IdempotencyKey: key}
}

// stringsOf reads a list of strings, as recorded in memory ([]string) or
// read back through JSON ([]any); empty is nil.
func stringsOf(value any) []string {
	var out []string
	switch list := value.(type) {
	case []string:
		out = append(out, list...)
	case []any:
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
