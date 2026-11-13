package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func groupOf(g *operatorv1.Group) application.Group {
	group := application.Group{TenantID: g.GetTenantId(), Name: g.GetName(), Members: []application.GroupMember{}}
	for _, m := range g.GetMembers() {
		group.Members = append(group.Members, application.GroupMember{
			BotID: m.GetBotId(), Name: m.GetName(), Status: m.GetStatus(), Purpose: m.GetPurpose(), Tools: m.GetTools(), Where: m.GetWhere(),
			Placement: placementSnapshot(m.GetPlacement()), Groups: m.GetGroups(), TakesDelegation: m.GetTakesDelegation(),
		})
	}
	return group
}

func groupTopicOf(t *operatorv1.GroupTopic) application.GroupTopic {
	topic := application.GroupTopic{
		Name: t.GetName(), Publishers: t.GetPublishers(), Declared: t.GetDeclaredPublishers(), Listeners: []application.TopicListener{},
		LastAt: timePtr(t.GetLastAt()), Count: int(t.GetCount()), NobodyListens: t.GetNobodyListens(),
	}
	for _, l := range t.GetListeners() {
		topic.Listeners = append(topic.Listeners, application.TopicListener{BotID: l.GetBotId(), Topic: l.GetTopic(), Mode: l.GetMode(), Status: l.GetStatus()})
	}
	return topic
}

func publishRequest(c application.PublishGroupEventCommand) (*operatorv1.PublishGroupEventRequest, error) {
	data, err := structOf("data", c.Data)
	if err != nil {
		return nil, err
	}
	return &operatorv1.PublishGroupEventRequest{
		Name: c.Group, Tenant: c.Tenant, EventName: c.Name, Message: c.Message, Data: data, To: c.To, IdempotencyKey: c.IdempotencyKey,
	}, nil
}

func publishAnswerOf(r *operatorv1.PublishGroupEventResponse) application.PublishAnswer {
	answer := application.PublishAnswer{
		EventID: r.GetEventId(), Name: r.GetName(), Severity: domain.Severity(r.GetSeverity()), Created: r.GetCreated(),
		Deliveries: []application.EventDelivery{}, Skipped: []application.EventSkip{},
	}
	for _, d := range r.GetDeliveries() {
		answer.Deliveries = append(answer.Deliveries, application.EventDelivery{
			BotID: d.GetBotId(), Mode: d.GetMode(), TaskID: d.GetTaskId(), MessageID: d.GetMessageId(), Trigger: d.GetTrigger(),
			Created: d.GetCreated(), Urgent: d.GetUrgent(), Degraded: d.GetDegraded(), CoalescedInto: d.GetCoalescedInto(),
		})
	}
	for _, s := range r.GetSkipped() {
		answer.Skipped = append(answer.Skipped, application.EventSkip{BotID: s.GetBotId(), Reason: s.GetReason(), Message: s.GetMessage()})
	}
	return answer
}
