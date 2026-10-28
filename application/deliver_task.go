package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// deliverTask makes the teammate's task under the emitter's trace id and
// lineage, so depth and the chain cut a loop as they cut a delegation's;
// urgent when the event interrupts the teammate.
func (uc EmitEvent) deliverTask(ctx context.Context, e emission, mate domain.Bot, sub domain.Subscription, urgent bool) (*EventDelivery, *EventSkip, error) {
	if !mate.AcceptsTasks() {
		return skipped(mate, domain.RefusedTargetUnavailable, "bot "+mate.ID().String()+" is "+mate.Status().String()+" and takes no task")
	}
	// A critical always lands: it is never coalesced and never degraded.
	if e.severity != domain.SeverityCritical {
		pending, found, err := pendingTaskOf(ctx, uc.deps, mate, e.name)
		if err != nil {
			return nil, nil, err
		}
		if found {
			return uc.deliverNote(ctx, e, mate, sub, "", pending.ID().String())
		}
		full, err := intakeFull(ctx, uc.deps, mate)
		if err != nil {
			return nil, nil, err
		}
		if full {
			return uc.deliverNote(ctx, e, mate, sub, DegradedIntakeRate, "")
		}
	}
	limit := eventChainDepthLimit(e.bot, e.task)
	depth := e.task.Depth() + 1
	if depth > limit {
		return skipped(mate, domain.RefusedDepthExceeded, fmt.Sprintf("a task of %s would be at depth %d; this chain allows %d", mate.ID(), depth, limit))
	}
	lineage, _ := e.task.Lineage()
	if lineage.Visited(mate.ID()) {
		return skipped(mate, domain.RefusedCycle, fmt.Sprintf("bot %s is already on this chain (%s): telling it would loop", mate.ID(), strings.Join(append(lineage.Chain, e.bot.ID().String()), " → ")))
	}
	template := sub.ObjectiveTemplate()
	if template == "" {
		template = domain.DefaultEventObjective
	}
	objective, err := domain.RenderObjective(template, e.fields())
	if err != nil {
		return skipped(mate, "objective", "the objective of "+mate.ID().String()+"'s subscription "+sub.TriggerName()+" does not render: "+err.Error())
	}
	trigger, err := domain.NewTriggerRef(domain.TriggerEvent.String(), sub.TriggerName())
	if err != nil {
		return nil, nil, err
	}
	key, err := domain.NewIdempotencyKey("event:" + e.id + ":" + mate.ID().String())
	if err != nil {
		return nil, nil, err
	}
	now := uc.deps.Clock.Now()
	child := domain.TaskLineage{
		ParentTaskID: e.task.ID().String(), ParentExecutionID: e.execution.ID().String(), ParentBotID: e.bot.ID().String(),
		Depth: depth, MaxDepth: limit, Chain: append(lineage.Chain, e.bot.ID().String()),
		Grant: domain.DelegationGrant{Deadline: now.Add(mate.Budgets().TaskTimeoutOr(DefaultTaskTimeout))}, EventID: e.id,
	}
	input, err := json.Marshal(e.fields())
	if err != nil {
		return nil, nil, err
	}
	s := submission{
		action: "task.event", bot: mate, trigger: trigger, objective: objective,
		key: key, correlation: e.task.CorrelationID(), input: input, lineage: &child,
	}
	if urgent {
		s.priority, s.why = domain.PriorityUrgent, "a "+e.severity.String()+" from its teammate "+e.bot.ID().String()+", event "+e.name.String()
	}
	created, err := submitTask(ctx, uc.deps, s)
	if err != nil {
		return nil, nil, err
	}
	delivery := &EventDelivery{BotID: mate.ID().String(), Mode: domain.ModeTask.String(), TaskID: created.Task.ID().String(), Trigger: sub.TriggerName(), Created: created.Created, Urgent: created.Task.IsUrgent()}
	if created.Created {
		if err := uc.recordHeard(ctx, e, mate, *delivery); err != nil {
			return nil, nil, err
		}
	}
	return delivery, nil, nil
}
