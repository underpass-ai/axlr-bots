package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func skillPinSnapshots(pins []*operatorv1.SkillPin) []domain.SkillPinSnapshot {
	if len(pins) == 0 {
		return nil
	}
	out := make([]domain.SkillPinSnapshot, len(pins))
	for i, pin := range pins {
		out[i] = domain.SkillPinSnapshot{Name: pin.GetName(), Version: pin.GetVersion(), Catalog: pin.GetCatalog()}
	}
	return out
}

func skillPinMessages(pins []domain.SkillPinSnapshot) []*operatorv1.SkillPin {
	out := make([]*operatorv1.SkillPin, len(pins))
	for i, pin := range pins {
		out[i] = &operatorv1.SkillPin{Name: pin.Name, Version: pin.Version, Catalog: pin.Catalog}
	}
	return out
}
