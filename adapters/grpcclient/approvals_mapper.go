package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func approvalsSnapshot(a *operatorv1.Approvals) domain.ApprovalsSnapshot {
	snapshot := domain.ApprovalsSnapshot{
		TTL: durationText(a.GetTtl()), OnExpire: a.GetOnExpire(),
		AutoApprove: toolRuleSnapshots(a.GetAutoApprove()), Approvers: a.GetApprovers(),
	}
	for _, target := range a.GetNotify() {
		snapshot.Notify = append(snapshot.Notify, domain.NotifyTargetSnapshot{
			Type: target.GetType(), Name: target.GetName(), URL: target.GetUrl(), Secret: target.GetSecret(), Command: target.GetCommand(),
		})
	}
	return snapshot
}

func approvalsMessage(a domain.ApprovalsSnapshot) (*operatorv1.Approvals, error) {
	ttl, err := durationOf("approvals.ttl", a.TTL)
	if err != nil {
		return nil, err
	}
	message := &operatorv1.Approvals{Ttl: ttl, OnExpire: a.OnExpire, AutoApprove: toolRuleMessages(a.AutoApprove), Approvers: a.Approvers}
	for _, target := range a.Notify {
		message.Notify = append(message.Notify, &operatorv1.NotifyTarget{Type: target.Type, Name: target.Name, Url: target.URL, Secret: target.Secret, Command: target.Command})
	}
	return message, nil
}
