package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func subscriptionSnapshots(subscriptions []*operatorv1.Subscription) []domain.SubscriptionSnapshot {
	if len(subscriptions) == 0 {
		return nil
	}
	out := make([]domain.SubscriptionSnapshot, len(subscriptions))
	for i, s := range subscriptions {
		out[i] = domain.SubscriptionSnapshot{
			Topic: s.GetTopic(), Mode: s.GetMode(), From: s.GetFrom(), Urgent: s.GetUrgent(), Objective: s.GetObjective(),
			Status: s.GetStatus(), AskedBy: s.GetAskedBy(), Why: s.GetWhy(), AskedAt: timePtr(s.GetAskedAt()),
		}
	}
	return out
}

func subscriptionMessages(subscriptions []domain.SubscriptionSnapshot) []*operatorv1.Subscription {
	out := make([]*operatorv1.Subscription, len(subscriptions))
	for i, s := range subscriptions {
		out[i] = &operatorv1.Subscription{
			Topic: s.Topic, Mode: s.Mode, From: s.From, Urgent: s.Urgent, Objective: s.Objective,
			Status: s.Status, AskedBy: s.AskedBy, Why: s.Why,
		}
		if s.AskedAt != nil {
			out[i].AskedAt = stampOf(*s.AskedAt)
		}
	}
	return out
}
