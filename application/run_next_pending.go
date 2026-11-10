package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// RunNextPending runs the oldest Pending task once, through RunTask, in
// the control plane's own process: the single-process mode. Only a bot
// with an empty placement runs there, and only while no Online node may
// run it; a task whose bot an Online node may run is left to that node
// (NextAssignment), and a placed bot's task waits for a node its
// placement admits, however long it is away (QA L10, 5 November 2026). The
// daemon's worker calls it on every tick, so a task submitted by another
// process (over the socket or straight into the state directory while
// the daemon was down) is picked up without a hand-off message.
type RunNextPending struct {
	deps Dependencies
}

// NewRunNextPending wires the use case.
func NewRunNextPending(deps Dependencies) RunNextPending {
	return RunNextPending{deps: deps}
}

// Execute runs the oldest Pending task and returns its result with ran
// true; with no Pending task it returns ran false and nothing else.
func (uc RunNextPending) Execute(ctx context.Context) (RunTaskResult, bool, error) {
	id, found, err := uc.Pick(ctx)
	if err != nil || !found {
		return RunTaskResult{}, false, err
	}
	result, err := NewRunTask(uc.deps).Execute(ctx, id)
	if err != nil {
		return RunTaskResult{}, true, err
	}
	return result, true, nil
}

// Pick chooses the oldest Pending task of a bot with an empty placement
// that no Online node may run, without running it. A placed bot's task
// is passed over: it waits for its node, with the control plane's tools
// and workspace never standing in for the node's. A task whose bot takes
// no task now (Paused, not yet enabled) or is gone is passed over too,
// as NextAssignment passes it over: RunTask would refuse it, it would
// stay Pending and stay the oldest, and every task behind it would wait
// for ever. A question's task never runs in process: it is answered from
// a team's store, which only a node opens.
func (uc RunNextPending) Pick(ctx context.Context) (domain.TaskID, bool, error) {
	pending, err := uc.deps.Tasks.ListByStatus(ctx, domain.TaskPending)
	if err != nil {
		return "", false, err
	}
	nodes, err := uc.deps.Nodes.List(ctx)
	if err != nil {
		return "", false, err
	}
	var oldest *domain.Task
	for i := range pending {
		bot, err := uc.deps.Bots.Get(ctx, pending[i].BotID())
		if domain.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		if !bot.AcceptsTasks() || !bot.Placement().IsEmpty() || hasOnlineNode(bot, pending[i].Skills(), nodes) {
			continue
		}
		if pending[i].Trigger().Type == domain.TriggerQuestion {
			// The control plane has no team's store: a question waits for
			// a node that opens it.
			continue
		}
		if oldest == nil || pending[i].CreatedAt().Before(oldest.CreatedAt()) {
			oldest = &pending[i]
		}
	}
	if oldest == nil {
		return "", false, nil
	}
	return oldest.ID(), true, nil
}

// hasOnlineNode reports whether an Online node may run a task of the bot
// that was submitted with the skills.
func hasOnlineNode(bot domain.Bot, skills domain.SkillPins, nodes []domain.Node) bool {
	for _, node := range nodes {
		if nodeCanRun(bot, skills, node) {
			return true
		}
	}
	return false
}

// nodeCanRun reports whether the node may take a task of the bot: it is
// Online, the bot's placement admits it, it has the bot's runtime and,
// for a task submitted with skills, it can read them. The skills are the
// task's, not the bot's: they are what the run will ask for.
// NextAssignment asks it of the node that polls; Pick asks it of every
// node for a bot with an empty placement, which runs in process while
// no node may run it (none Online, or the only Online one lacks its
// runtime). A placed bot's task no node may run stays Pending until one
// may, and placementWait says what it waits for.
func nodeCanRun(bot domain.Bot, skills domain.SkillPins, node domain.Node) bool {
	return node.IsOnline() && bot.Placement().Admits(node) && node.Capabilities().Runs(bot.Runtime().Kind()) && (skills.IsEmpty() || node.Capabilities().SkillsSupported)
}
