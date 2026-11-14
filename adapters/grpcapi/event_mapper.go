package grpcapi

import (
	"encoding/json"

	"google.golang.org/protobuf/types/known/structpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// eventToProto is one entry of a timeline. The payload goes through JSON
// so that whatever the event holds (a list of strings, a nested map)
// becomes a Struct.
func eventToProto(e domain.Event) *operatorv1.Event {
	return &operatorv1.Event{
		EventId: string(e.ID), SchemaVersion: string(e.SchemaVersion), Type: string(e.Type), TenantId: string(e.TenantID),
		BotId: string(e.BotID), GroupName: e.GroupName, NodeId: string(e.NodeID), TaskId: string(e.TaskID),
		ExecutionId: string(e.ExecutionID), CorrelationId: string(e.CorrelationID), CreatedAt: timestamp(e.CreatedAt),
		Payload: structOf(e.Payload),
	}
}

func eventsListToProto(events []domain.Event) []*operatorv1.Event {
	out := make([]*operatorv1.Event, 0, len(events))
	for _, e := range events {
		out = append(out, eventToProto(e))
	}
	return out
}

// structOf is the map as a Struct; a map that is not JSON is absent.
func structOf(values map[string]any) *structpb.Struct {
	if len(values) == 0 {
		return nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	out := &structpb.Struct{}
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil
	}
	return out
}
