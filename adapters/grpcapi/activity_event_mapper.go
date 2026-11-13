package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
	"google.golang.org/protobuf/types/known/structpb"
)

// activityEvent maps an event to the wire; a payload that cannot be a
// protobuf struct is left out rather than failing the answer.
func activityEvent(e domain.Event) *operatorv1.Event {
	out := &operatorv1.Event{
		EventId: e.ID.String(), SchemaVersion: string(e.SchemaVersion), Type: string(e.Type),
		TenantId: string(e.TenantID), BotId: string(e.BotID), GroupName: e.GroupName, NodeId: string(e.NodeID),
		TaskId: string(e.TaskID), ExecutionId: string(e.ExecutionID), CorrelationId: string(e.CorrelationID),
		CreatedAt: activityTime(e.CreatedAt),
	}
	if len(e.Payload) > 0 {
		if payload, err := structpb.NewStruct(jsonSafe(e.Payload)); err == nil {
			out.Payload = payload
		}
	}
	return out
}

// activityEvents maps a timeline.
func activityEvents(events []domain.Event) []*operatorv1.Event {
	out := make([]*operatorv1.Event, len(events))
	for i, e := range events {
		out[i] = activityEvent(e)
	}
	return out
}
