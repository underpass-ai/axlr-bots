package application

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// DeletedReason is the reason a deleted bot's tasks keep.
const DeletedReason = "bot deleted"

// DeleteBot removes a bot and ends what it left going, in an order that
// never leaves a record pointing at a bot that is gone: every task it
// has not finished is
// cancelled as a stop cancels it (a run in progress is interrupted, a
// node's run stops at its next lease renewal), then the delegated
// children it asked for, down the chain (deleting is stronger than
// cancelling a parent), then its timers and the notifications of its
// approvals; last the bot's record. Its tasks, events, audit and
// artifacts stay, as history. A task that cannot be cancelled stops the
// delete before the record goes: running it again is safe.
type DeleteBot struct {
	deps Dependencies
	rec  recorder
}

// NewDeleteBot wires the use case.
func NewDeleteBot(deps Dependencies) DeleteBot {
	return DeleteBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute deletes the bot and answers what it did.
func (uc DeleteBot) Execute(ctx context.Context, id domain.BotID) (DeleteBotResult, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return DeleteBotResult{}, err
	}
	result := DeleteBotResult{Bot: bot}
	tasks, err := uc.deps.Tasks.List(ctx)
	if err != nil {
		return DeleteBotResult{}, err
	}
	if result.Cancelled, err = uc.cancelOwn(ctx, bot, tasks); err != nil {
		return DeleteBotResult{}, err
	}
	if result.ChildrenCancelled, err = uc.cancelChildren(ctx, bot, tasks); err != nil {
		return DeleteBotResult{}, err
	}
	if result.TimersRemoved, err = uc.removeTimers(ctx, id); err != nil {
		return DeleteBotResult{}, err
	}
	if result.NotificationsDropped, err = uc.dropNotifications(ctx, id); err != nil {
		return DeleteBotResult{}, err
	}
	if result.NamedBy, err = namedBy(ctx, uc.deps, id); err != nil {
		return DeleteBotResult{}, err
	}
	if err := uc.deps.Bots.Delete(ctx, id); err != nil {
		return DeleteBotResult{}, err
	}
	if err := uc.rec.event(ctx, domain.EventBotDeleted, bot, "", "", map[string]any{
		"cancelled": len(result.Cancelled), "childrenCancelled": len(result.ChildrenCancelled),
		"timersRemoved":        result.TimersRemoved,
		"notificationsDropped": result.NotificationsDropped, "namedBy": result.NamedBy,
	}); err != nil {
		return DeleteBotResult{}, err
	}
	details := map[string]string{
		"cancelled": strconv.Itoa(len(result.Cancelled)), "childrenCancelled": strconv.Itoa(len(result.ChildrenCancelled)),
		"timersRemoved": strconv.Itoa(result.TimersRemoved),
	}
	if err := uc.rec.audit(ctx, "bot.delete", id.String(), domain.AuditOK, details); err != nil {
		return DeleteBotResult{}, err
	}
	return result, nil
}

// cancelOwn cancels every task of the bot it has not finished.
func (uc DeleteBot) cancelOwn(ctx context.Context, bot domain.Bot, tasks []domain.Task) ([]domain.TaskID, error) {
	cancelled := []domain.TaskID{}
	for _, task := range tasks {
		if task.BotID() != bot.ID() || task.Status().IsTerminal() {
			continue
		}
		if _, err := stopTask(ctx, uc.deps, bot, task, DeletedReason, domain.EventTaskCancelled, "task.cancel", map[string]any{"deleted": true}); err != nil {
			return nil, err
		}
		cancelled = append(cancelled, task.ID())
	}
	return cancelled, nil
}

// cancelChildren cancels the unfinished tasks the bot asked other bots
// for, and the ones those asked for in turn: a delegation whose parent
// is one of the bot's tasks, or a child already cancelled here. A task a
// teammate's event made is not a delegation and runs on.
func (uc DeleteBot) cancelChildren(ctx context.Context, bot domain.Bot, tasks []domain.Task) ([]domain.TaskID, error) {
	parents := map[string]bool{}
	for _, task := range tasks {
		if task.BotID() == bot.ID() {
			parents[task.ID().String()] = true
		}
	}
	reason := "the bot that asked for it, " + bot.ID().String() + ", was deleted"
	cancelled := []domain.TaskID{}
	for found := true; found; {
		found = false
		for _, task := range tasks {
			lineage, ok := task.Lineage()
			if !ok || lineage.EventID != "" || !parents[lineage.ParentTaskID] || parents[task.ID().String()] || task.Status().IsTerminal() {
				continue
			}
			parents[task.ID().String()], found = true, true
			owner, err := uc.deps.Bots.Get(ctx, task.BotID())
			if domain.IsNotFound(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if _, err := stopTask(ctx, uc.deps, owner, task, reason, domain.EventTaskCancelled, "task.cancel", map[string]any{"deletedBot": bot.ID().String()}); err != nil {
				return nil, err
			}
			cancelled = append(cancelled, task.ID())
		}
	}
	return cancelled, nil
}

// removeTimers deletes the bot's timers, of its triggers now and of any
// it had before.
func (uc DeleteBot) removeTimers(ctx context.Context, id domain.BotID) (int, error) {
	timers, err := uc.deps.Timers.List(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, timer := range timers {
		if timer.BotID() != id {
			continue
		}
		if err := uc.deps.Timers.Delete(ctx, timer.Key()); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// dropNotifications ends the bot's notifications still to be sent: the
// waits they announce were cancelled.
func (uc DeleteBot) dropNotifications(ctx context.Context, id domain.BotID) (int, error) {
	pending, err := uc.deps.Deliveries.ListPending(ctx)
	if err != nil {
		return 0, err
	}
	dropped := 0
	for _, delivery := range pending {
		if delivery.BotID() != id {
			continue
		}
		if err := delivery.Skip("the bot is deleted", uc.deps.Clock.Now()); err != nil {
			return dropped, err
		}
		if err := uc.deps.Deliveries.Save(ctx, delivery); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

// namedBy lists the other bots that name the bot: as a delegation
// target, or as a sender of a topic they listen to; by bot id, then
// topic.
func namedBy(ctx context.Context, deps Dependencies, id domain.BotID) ([]BotReference, error) {
	bots, err := deps.Bots.List(ctx)
	if err != nil {
		return nil, err
	}
	refs := []BotReference{}
	for _, other := range bots {
		if other.ID() == id {
			continue
		}
		if slices.Contains(other.Delegation().To(), id) {
			refs = append(refs, BotReference{BotID: other.ID().String(), How: ReferenceDelegation})
		}
		for _, sub := range other.Subscriptions().List() {
			if slices.Contains(sub.From(), id.String()) {
				refs = append(refs, BotReference{BotID: other.ID().String(), How: ReferenceSubscription, Topic: sub.Topic().String()})
			}
		}
	}
	slices.SortFunc(refs, func(a, b BotReference) int {
		if a.BotID != b.BotID {
			return strings.Compare(a.BotID, b.BotID)
		}
		return strings.Compare(a.Topic, b.Topic)
	})
	return refs, nil
}
