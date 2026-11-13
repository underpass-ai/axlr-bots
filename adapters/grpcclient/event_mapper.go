package grpcclient

import (
	"google.golang.org/protobuf/types/known/structpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func eventOf(e *operatorv1.Event) domain.Event {
	event := domain.Event{
		ID: domain.EventID(e.GetEventId()), SchemaVersion: domain.SchemaVersion(e.GetSchemaVersion()), Type: domain.EventType(e.GetType()),
		TenantID: domain.TenantID(e.GetTenantId()), BotID: domain.BotID(e.GetBotId()), GroupName: e.GetGroupName(), NodeID: domain.NodeID(e.GetNodeId()),
		TaskID: domain.TaskID(e.GetTaskId()), ExecutionID: domain.ExecutionID(e.GetExecutionId()), CorrelationID: domain.CorrelationID(e.GetCorrelationId()),
		CreatedAt: timeOf(e.GetCreatedAt()),
	}
	if payload := e.GetPayload(); payload != nil && len(payload.GetFields()) > 0 {
		event.Payload = payload.AsMap()
	}
	return event
}

// eventsOf is the events of an answer; an empty one is [], as the HTTP
// API answered.
func eventsOf(events []*operatorv1.Event) []domain.Event {
	out := make([]domain.Event, len(events))
	for i, event := range events {
		out[i] = eventOf(event)
	}
	return out
}

func structOf(field string, data map[string]any) (*structpb.Struct, error) {
	if data == nil {
		return nil, nil
	}
	out, err := structpb.NewStruct(data)
	if err != nil {
		return nil, domain.ValidationError{Field: field, Reason: "must be a JSON object: " + err.Error()}
	}
	return out, nil
}
