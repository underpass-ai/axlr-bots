package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Limits of a delegation.
const (
	// MinDelegationTime is the least time a child may be granted; a
	// parent with less left to share is refused budget_exhausted.
	MinDelegationTime = 30 * time.Second
	// DelegationWaitMargin is how long before its own deadline a parent
	// stops waiting for a child, so it still has time to answer.
	DelegationWaitMargin = 10 * time.Second
)

// executionDeadline is when the execution is stopped: when it started
// plus its bot's timeout, or the delegated task's own deadline when that
// comes first.
func executionDeadline(bot domain.Bot, task domain.Task, execution domain.Execution) time.Time {
	deadline := execution.StartedAt().Add(bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout))
	if lineage, ok := task.Lineage(); ok && lineage.Grant.Deadline.Before(deadline) {
		return lineage.Grant.Deadline
	}
	return deadline
}

// chainDepthLimit is the deepest a delegation from the task may go: the
// least of its chain's limit and its own bot's.
func chainDepthLimit(bot domain.Bot, task domain.Task) int {
	limit := bot.DelegationDepth()
	if lineage, ok := task.Lineage(); ok && lineage.MaxDepth < limit {
		limit = lineage.MaxDepth
	}
	return limit
}

// taskBudgets is what an execution of the task may spend: a delegated
// task's grant (the bot's own limit where the grant sets none), else
// the bot's budgets.
func taskBudgets(bot domain.Bot, task domain.Task) domain.Budgets {
	budgets := delegatedBudgets(bot, task)
	pinned, ok := task.Pinned()
	if !ok || !pinned.Unattended() {
		return budgets
	}
	// One apply per task (§8.1): a run acting alone has its reads and its
	// one write, whatever the bot's budget says.
	unit, _ := pinned.LadderUnit()
	tools := budgets.MaxToolCallsPerTask()
	if tools == 0 || tools > unit.MaxToolCalls {
		tools = unit.MaxToolCalls
	}
	capped, err := domain.NewBudgets(budgets.MaxModelCallsPerTask(), tools, budgets.MaxDelegationDepth(), budgets.TaskTimeout())
	if err != nil {
		return budgets
	}
	return capped
}

func delegatedBudgets(bot domain.Bot, task domain.Task) domain.Budgets {
	lineage, ok := task.Lineage()
	if !ok {
		return bot.Budgets()
	}
	own := bot.Budgets()
	model, tools := lineage.Grant.MaxModelCalls, lineage.Grant.MaxToolCalls
	if model == 0 {
		model = own.MaxModelCallsPerTask()
	}
	if tools == 0 {
		tools = own.MaxToolCallsPerTask()
	}
	budgets, err := domain.NewBudgets(model, tools, own.MaxDelegationDepth(), own.TaskTimeout())
	if err != nil {
		return own
	}
	return budgets
}

// shareOf is one limit of a child's grant: the share of what the parent
// has left of it (its pool less what it already granted), never more
// than the child bot's own limit. Where the parent has no limit the
// child has its own (0: none). False when the share is under one.
func shareOf(pool, granted int, share float64, own int) (int, bool) {
	if pool == 0 {
		return own, true
	}
	grant := int(math.Floor(share * float64(pool-granted)))
	if grant < 1 {
		return 0, false
	}
	if own > 0 && own < grant {
		grant = own
	}
	return grant, true
}

// evenShareOf is one limit of a child's grant under the even split: the
// share of the parent's whole pool divided among the most children it
// may ask for, never more than the child bot's own limit. Where the
// parent has no limit the child has its own (0: none). False when that
// is under one.
func evenShareOf(pool int, share float64, children, own int) (int, bool) {
	if pool == 0 {
		return own, true
	}
	grant := int(math.Floor(share * float64(pool) / float64(children)))
	if grant < 1 {
		return 0, false
	}
	if own > 0 && own < grant {
		grant = own
	}
	return grant, true
}

// limitOf is one limit of a child's grant under the delegation's split.
func limitOf(policy domain.Delegation, pool, granted, own int) (int, bool) {
	if policy.Split() == domain.SplitEven {
		return evenShareOf(pool, policy.BudgetShare(), policy.MaxChildren(), own)
	}
	return shareOf(pool, granted, policy.BudgetShare(), own)
}

// underOne says why a limit of the grant is under one, in the split's
// terms.
func underOne(policy domain.Delegation, limit string, pool, granted int) string {
	if policy.Split() == domain.SplitEven {
		return fmt.Sprintf("this task has %d %s, and %.2f of them split among %d children is under one each", pool, limit, policy.BudgetShare(), policy.MaxChildren())
	}
	return fmt.Sprintf("this task has %d of its %d %s left to share, and %.2f of them is under one", pool-granted, pool, limit, policy.BudgetShare())
}

// delegationKey is the child's idempotency key under its bot: the same
// ask from the same task finds the same child.
func delegationKey(parent domain.TaskID, bot domain.BotID, objective domain.Objective) (domain.IdempotencyKey, error) {
	sum := sha256.Sum256([]byte(bot.String() + "\n" + objective.String()))
	return domain.NewIdempotencyKey("delegation:" + parent.String() + ":" + hex.EncodeToString(sum[:12]))
}

// errTooLate is describe's answer for an execution whose delegated
// task has less than domain.MinRunWindow left: the caller misses the
// deadline instead of running it.
var errTooLate = errors.New("the delegation's deadline leaves no time to run")

// runTimeout is how long an execution of the task that starts now may
// run: the bot's timeout, or the time left to a delegated task's
// deadline when that is shorter. It answers false when that time is
// under domain.MinRunWindow: a node gets its timeout in whole seconds
// and reads zero as "no timeout", so a sub-second remainder would let
// the child run for the node's default (up to 15 minutes) past its
// grant.
func runTimeout(bot domain.Bot, task domain.Task, now time.Time) (time.Duration, bool) {
	timeout := bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout)
	if lineage, ok := task.Lineage(); ok {
		left := lineage.Grant.Deadline.Sub(now)
		if left < domain.MinRunWindow {
			return 0, false
		}
		if left < timeout {
			timeout = left
		}
	}
	return timeout, true
}

// allowedTools is the execution's tool allow-list: the task's (the bot's,
// or its incident phase's), the delegation tool when the bot may ask
// other bots, and the emit tool when it is in a team.
func allowedTools(allowed domain.AllowedTools, offer *DelegationOffer, events *EventsOffer) []string {
	tools := allowed.Names()
	if offer != nil && !allowed.Allows(DelegateToolName) {
		tools = append(tools, DelegateToolName)
	}
	if events != nil && !allowed.Allows(EmitEventToolName) {
		tools = append(tools, EmitEventToolName)
	}
	if events != nil && !allowed.Allows(SubscribeToolName) {
		tools = append(tools, SubscribeToolName)
	}
	return tools
}

// eventsOffer tells the node the bot may emit events to its teams, or
// nil when it is in none.
func eventsOffer(bot domain.Bot, mates []domain.Bot) *EventsOffer {
	if bot.Teams().Empty() {
		return nil
	}
	offer := &EventsOffer{Teammates: []string{}}
	for _, rule := range bot.Events().Rules() {
		offer.Names = append(offer.Names, rule.Pattern().String())
	}
	for _, mate := range mates {
		if mate.AcceptsTasks() {
			offer.Teammates = append(offer.Teammates, mate.ID().String())
		}
	}
	return offer
}

// delegationOffer tells the node the bot may delegate, to the bots its
// delegation names and its teammates that take its tasks, or nil when
// there is nobody to ask.
func delegationOffer(bot domain.Bot, task domain.Task, mates []domain.Bot) *DelegationOffer {
	targets := delegationTargets(bot, mates)
	if len(targets) == 0 {
		return nil
	}
	return &DelegationOffer{Targets: targets, Depth: task.Depth(), MaxDepth: chainDepthLimit(bot, task)}
}
