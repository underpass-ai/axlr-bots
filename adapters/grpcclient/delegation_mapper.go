package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func delegationSnapshot(d *operatorv1.Delegation) *domain.DelegationSnapshot {
	if d == nil {
		return nil
	}
	snapshot := &domain.DelegationSnapshot{To: d.GetTo(), MaxDepth: int(d.GetMaxDepth()), BudgetShare: d.GetBudgetShare(), Split: d.GetSplit(), MaxChildren: int(d.GetMaxChildren())}
	if d.FromTeam != nil {
		fromTeam := d.GetFromTeam()
		snapshot.FromTeam = &fromTeam
	}
	return snapshot
}

func delegationMessage(d *domain.DelegationSnapshot) *operatorv1.Delegation {
	if d == nil {
		return nil
	}
	return &operatorv1.Delegation{To: d.To, MaxDepth: int32(d.MaxDepth), BudgetShare: d.BudgetShare, Split: d.Split, MaxChildren: int32(d.MaxChildren), FromTeam: d.FromTeam}
}
