package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AskSubscription is a running execution asking, through its node, that
// its bot hear a topic of its teams. The control plane adds a pending
// subscription for a person to approve or deny from the panel; nothing is
// heard until then.
type AskSubscription struct {
	deps Dependencies
	rec  recorder
}

// NewAskSubscription wires the use case.
func NewAskSubscription(deps Dependencies) AskSubscription {
	return AskSubscription{deps: deps, rec: recorder{deps: deps}}
}

// Execute adds the pending subscription, or answers why not.
func (uc AskSubscription) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, command SubscribeCommand) (SubscribeAnswer, error) {
	execution, err := runningExecutionOf(ctx, uc.deps, node, executionID, "ask for a subscription")
	if err != nil {
		return SubscribeAnswer{}, err
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return SubscribeAnswer{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return SubscribeAnswer{}, err
	}
	refuse := func(reason, message string) (SubscribeAnswer, error) {
		details := map[string]string{"node": node.String(), "execution": executionID.String(), "topic": command.Topic, "reason": reason}
		if err := uc.rec.audit(ctx, "subscription.ask", bot.ID().String(), domain.AuditDenied, details); err != nil {
			return SubscribeAnswer{}, err
		}
		return SubscribeAnswer{Topic: command.Topic, Refused: &SubscribeRefusal{Reason: reason, Message: message}}, nil
	}
	if bot.Teams().Empty() {
		return refuse(domain.RefusedNoTeam, "bot "+bot.ID().String()+" is in no team: there is nothing to hear")
	}
	subs, reason, err := bot.Subscriptions().Ask(command.Topic, bot.ID(), command.Why, uc.deps.Clock.Now())
	if err != nil {
		return SubscribeAnswer{}, err
	}
	switch reason {
	case domain.RefusedAlreadySubscribed:
		return refuse(reason, "bot "+bot.ID().String()+" listens to "+command.Topic+" already, or asked for it and waits for a person")
	case domain.RefusedTooMany:
		return refuse(reason, "bot "+bot.ID().String()+" listens to 16 topics, its most; ask your operator to remove one")
	}
	bot.SetSubscriptions(subs)
	bot.Touch(uc.deps.Clock.Now())
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return SubscribeAnswer{}, err
	}
	asked, _ := subs.Find(subs.List()[len(subs.List())-1].Topic())
	payload := map[string]any{"subscription": asked.Snapshot(), "topic": asked.Topic().String(), "why": asked.Snapshot().Why, "taskId": task.ID().String()}
	if err := uc.rec.executionEvent(ctx, domain.EventSubscriptionAsked, bot, task.ID(), executionID, task.CorrelationID(), payload); err != nil {
		return SubscribeAnswer{}, err
	}
	details := map[string]string{"node": node.String(), "execution": executionID.String(), "topic": asked.Topic().String()}
	if err := uc.rec.audit(ctx, "subscription.ask", bot.ID().String(), domain.AuditOK, details); err != nil {
		return SubscribeAnswer{}, err
	}
	return SubscribeAnswer{Topic: asked.Topic().String(), Status: domain.SubscriptionPending.String()}, nil
}
