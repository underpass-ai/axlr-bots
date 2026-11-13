package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func triggerSnapshots(triggers []*operatorv1.Trigger) []domain.TriggerSnapshot {
	out := make([]domain.TriggerSnapshot, len(triggers))
	for i, t := range triggers {
		out[i] = domain.TriggerSnapshot{
			Type: t.GetType(), Name: t.GetName(), Objective: t.GetObjective(), Secret: t.GetSecret(), SigningKey: t.GetSigningKey(),
			Every: durationText(t.GetEvery()), Cron: t.GetCron(), Events: t.GetEvents(), Interrupt: t.GetInterrupt(),
		}
	}
	return out
}

func triggerMessages(triggers []domain.TriggerSnapshot) ([]*operatorv1.Trigger, error) {
	out := make([]*operatorv1.Trigger, len(triggers))
	for i, t := range triggers {
		every, err := durationOf("triggers.every", t.Every)
		if err != nil {
			return nil, err
		}
		out[i] = &operatorv1.Trigger{
			Type: t.Type, Name: t.Name, Objective: t.Objective, Secret: t.Secret, SigningKey: t.SigningKey,
			Every: every, Cron: t.Cron, Events: t.Events, Interrupt: t.Interrupt,
		}
	}
	return out, nil
}
