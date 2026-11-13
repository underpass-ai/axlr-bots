package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func botEventsSnapshot(e *operatorv1.Events) *domain.EventsSnapshot {
	if e == nil {
		return nil
	}
	snapshot := &domain.EventsSnapshot{PerMinute: int(e.GetPerMinute()), IntakePerMinute: int(e.GetIntakePerMinute()), MaxDepth: int(e.GetMaxDepth())}
	for _, rule := range e.GetEmit() {
		snapshot.Emit = append(snapshot.Emit, domain.EmitRuleSnapshot{Name: rule.GetName(), MaxSeverity: rule.GetMaxSeverity()})
	}
	if e.Interruptible != nil {
		interruptible := e.GetInterruptible()
		snapshot.Interruptible = &interruptible
	}
	return snapshot
}

func botEventsMessage(e *domain.EventsSnapshot) *operatorv1.Events {
	if e == nil {
		return nil
	}
	message := &operatorv1.Events{PerMinute: int32(e.PerMinute), IntakePerMinute: int32(e.IntakePerMinute), MaxDepth: int32(e.MaxDepth), Interruptible: e.Interruptible}
	for _, rule := range e.Emit {
		message.Emit = append(message.Emit, &operatorv1.EmitRule{Name: rule.Name, MaxSeverity: rule.MaxSeverity})
	}
	return message
}
