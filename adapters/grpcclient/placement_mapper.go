package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func placementSnapshot(p *operatorv1.Placement) domain.PlacementSnapshot {
	snapshot := domain.PlacementSnapshot{Nodes: p.GetNodes(), Selector: p.GetSelector()}
	if len(snapshot.Selector) == 0 {
		snapshot.Selector = nil
	}
	return snapshot
}

func placementMessage(p domain.PlacementSnapshot) *operatorv1.Placement {
	return &operatorv1.Placement{Nodes: p.Nodes, Selector: p.Selector}
}
