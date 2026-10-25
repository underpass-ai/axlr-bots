package application

import (
	"crypto/sha256"
	"encoding/hex"
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

// delegationKey is the child's idempotency key under its bot: the same
// ask from the same task finds the same child.
func delegationKey(parent domain.TaskID, bot domain.BotID, objective domain.Objective) (domain.IdempotencyKey, error) {
	sum := sha256.Sum256([]byte(bot.String() + "\n" + objective.String()))
	return domain.NewIdempotencyKey("delegation:" + parent.String() + ":" + hex.EncodeToString(sum[:12]))
}

// runTimeout is how long an execution of the task that starts now may
// run: the bot's timeout, or the time left to a delegated task's
// deadline when that is shorter.
func runTimeout(bot domain.Bot, task domain.Task, now time.Time) time.Duration {
	timeout := bot.Budgets().TaskTimeoutOr(DefaultTaskTimeout)
	if lineage, ok := task.Lineage(); ok {
		if left := lineage.Grant.Deadline.Sub(now); left < timeout {
			timeout = left
		}
	}
	return timeout
}

// allowedTools is the execution's tool allow-list: the bot's, and the
// delegation tool when the bot may ask other bots.
func allowedTools(bot domain.Bot) []string {
	tools := bot.AllowedTools().Names()
	if bot.Delegation().Enabled() && !bot.AllowedTools().Allows(DelegateToolName) {
		tools = append(tools, DelegateToolName)
	}
	return tools
}

// delegationOffer tells the node the bot may delegate, or nil.
func delegationOffer(bot domain.Bot, task domain.Task) *DelegationOffer {
	if !bot.Delegation().Enabled() {
		return nil
	}
	targets := make([]string, 0, len(bot.Delegation().To()))
	for _, id := range bot.Delegation().To() {
		targets = append(targets, id.String())
	}
	return &DelegationOffer{Targets: targets, Depth: task.Depth(), MaxDepth: chainDepthLimit(bot, task)}
}
