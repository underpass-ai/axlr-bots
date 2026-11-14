package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
)

func groupToProto(g application.Group) *operatorv1.Group {
	members := make([]*operatorv1.GroupMember, 0, len(g.Members))
	for _, m := range g.Members {
		members = append(members, &operatorv1.GroupMember{
			BotId: m.BotID, Name: m.Name, Status: m.Status, Purpose: m.Purpose, Tools: m.Tools, Where: m.Where,
			Placement: placementToProto(m.Placement), Groups: m.Groups, TakesDelegation: m.TakesDelegation,
		})
	}
	return &operatorv1.Group{TenantId: g.TenantID, Name: g.Name, Members: members}
}

func groupTopicsToProto(topics []application.GroupTopic) []*operatorv1.GroupTopic {
	out := make([]*operatorv1.GroupTopic, 0, len(topics))
	for _, t := range topics {
		topic := &operatorv1.GroupTopic{
			Name: t.Name, Publishers: t.Publishers, DeclaredPublishers: t.Declared, LastAt: timestampOf(t.LastAt),
			Count: int32(t.Count), NobodyListens: t.NobodyListens,
		}
		for _, l := range t.Listeners {
			topic.Listeners = append(topic.Listeners, &operatorv1.TopicListener{BotId: l.BotID, Topic: l.Topic, Mode: l.Mode, Status: l.Status})
		}
		out = append(out, topic)
	}
	return out
}

func publishAnswerToProto(a application.PublishAnswer) *operatorv1.PublishGroupEventResponse {
	response := &operatorv1.PublishGroupEventResponse{EventId: a.EventID, Name: a.Name, Severity: string(a.Severity), Created: a.Created}
	for _, d := range a.Deliveries {
		response.Deliveries = append(response.Deliveries, &operatorv1.EventDelivery{
			BotId: d.BotID, Mode: d.Mode, TaskId: d.TaskID, MessageId: d.MessageID, Trigger: d.Trigger,
			Created: d.Created, Urgent: d.Urgent, Degraded: d.Degraded, CoalescedInto: d.CoalescedInto,
		})
	}
	for _, s := range a.Skipped {
		response.Skipped = append(response.Skipped, &operatorv1.EventSkip{BotId: s.BotID, Reason: s.Reason, Message: s.Message})
	}
	return response
}
