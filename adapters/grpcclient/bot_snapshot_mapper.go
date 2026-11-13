package grpcclient

import (
	"encoding/json"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// botSnapshot is a bot as the API answers it, as the snapshot the
// command prints.
func botSnapshot(bot *operatorv1.Bot) domain.BotSnapshot {
	snapshot := domain.BotSnapshot{
		ID: bot.GetId(), TenantID: bot.GetTenantId(), Name: bot.GetName(), Owner: bot.GetOwner(), Mission: bot.GetMission(),
		AllowedTools: bot.GetAllowedTools(), Model: modelPolicySnapshot(bot.GetModel()), Budgets: budgetsSnapshot(bot.GetBudgets()),
		Level: bot.GetLevel(), ToolRules: toolRulesSnapshot(bot.GetToolRules()), Triggers: triggerSnapshots(bot.GetTriggers()),
		Placement: placementSnapshot(bot.GetPlacement()), Approvals: approvalsSnapshot(bot.GetApprovals()),
		Delegation: delegationSnapshot(bot.GetDelegation()), Groups: bot.GetGroups(), Events: botEventsSnapshot(bot.GetEvents()),
		Subscriptions: subscriptionSnapshots(bot.GetSubscriptions()), Runtime: runtimeSnapshot(bot.GetRuntime()),
		Skills: skillPinSnapshots(bot.GetSkills()), Version: bot.GetVersion(), Status: bot.GetStatus(),
		CreatedAt: timeOf(bot.GetCreatedAt()), UpdatedAt: timeOf(bot.GetUpdatedAt()),
	}
	if snapshot.AllowedTools == nil {
		snapshot.AllowedTools = []string{}
	}
	if staged := bot.GetStaged(); staged != nil {
		snapshot.Staged = &domain.StagedDefinitionSnapshot{Version: staged.GetVersion(), StagedAt: timeOf(staged.GetStagedAt())}
		if change := staged.GetChange(); len(change) > 0 {
			snapshot.Staged.Change = json.RawMessage(change)
		}
	}
	return snapshot
}

func botSnapshots(bots []*operatorv1.Bot) []domain.BotSnapshot {
	out := make([]domain.BotSnapshot, len(bots))
	for i, bot := range bots {
		out[i] = botSnapshot(bot)
	}
	return out
}
