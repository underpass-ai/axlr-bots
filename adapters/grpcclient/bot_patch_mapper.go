package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// botPatchMessage is a patch on the wire. A field that is not set stays
// absent; a list set to empty is the wrapper with no values, which
// clears it.
func botPatchMessage(p application.BotPatch) (*operatorv1.BotPatch, error) {
	message := &operatorv1.BotPatch{Version: p.Version, Mission: p.Mission, Level: p.Level}
	if p.AllowedTools != nil {
		message.AllowedTools = &operatorv1.StringList{Values: *p.AllowedTools}
	}
	if p.ToolRules != nil {
		message.ToolRules = toolRulesMessage(*p.ToolRules)
	}
	if p.Triggers != nil {
		triggers, err := triggerMessages(*p.Triggers)
		if err != nil {
			return nil, err
		}
		message.Triggers = &operatorv1.TriggerList{Values: triggers}
	}
	if p.Placement != nil {
		message.Placement = placementMessage(*p.Placement)
	}
	if p.Approvals != nil {
		approvals, err := approvalsMessage(*p.Approvals)
		if err != nil {
			return nil, err
		}
		message.Approvals = approvals
	}
	if p.AutoApprove != nil {
		message.AutoApprove = &operatorv1.ToolRuleList{Values: toolRuleMessages(*p.AutoApprove)}
	}
	message.Delegation = delegationMessage(p.Delegation)
	if p.Groups != nil {
		message.Groups = &operatorv1.StringList{Values: *p.Groups}
	}
	message.Events = botEventsMessage(p.Events)
	if p.Subscriptions != nil {
		message.Subscriptions = &operatorv1.SubscriptionList{Values: subscriptionMessages(*p.Subscriptions)}
	}
	message.Runtime = runtimeMessage(p.Runtime)
	if p.Skills != nil {
		message.Skills = &operatorv1.SkillPinList{Values: skillPinMessages(*p.Skills)}
	}
	if p.Model != nil {
		message.Model = modelPolicyMessage(*p.Model)
	}
	if p.Budgets != nil {
		message.Budgets = budgetsMessage(*p.Budgets)
	}
	return message, nil
}

// botPatch is a patch as the API answers it (the staged change): the
// presence of each field is kept.
func botPatch(m *operatorv1.BotPatch) application.BotPatch {
	patch := application.BotPatch{Version: m.Version, Mission: m.Mission, Level: m.Level}
	if m.GetAllowedTools() != nil {
		values := nonNil(m.GetAllowedTools().GetValues())
		patch.AllowedTools = &values
	}
	if m.GetToolRules() != nil {
		rules := toolRulesSnapshot(m.GetToolRules())
		patch.ToolRules = &rules
	}
	if m.GetTriggers() != nil {
		triggers := triggerSnapshots(m.GetTriggers().GetValues())
		patch.Triggers = &triggers
	}
	if m.GetPlacement() != nil {
		placement := placementSnapshot(m.GetPlacement())
		patch.Placement = &placement
	}
	if m.GetApprovals() != nil {
		approvals := approvalsSnapshot(m.GetApprovals())
		patch.Approvals = &approvals
	}
	if m.GetAutoApprove() != nil {
		rules := application.AutoApproveRules(toolRuleSnapshots(m.GetAutoApprove().GetValues()))
		if rules == nil {
			rules = application.AutoApproveRules{}
		}
		patch.AutoApprove = &rules
	}
	patch.Delegation = delegationSnapshot(m.GetDelegation())
	if m.GetGroups() != nil {
		values := nonNil(m.GetGroups().GetValues())
		patch.Groups = &values
	}
	patch.Events = botEventsSnapshot(m.GetEvents())
	if m.GetSubscriptions() != nil {
		subscriptions := subscriptionSnapshots(m.GetSubscriptions().GetValues())
		if subscriptions == nil {
			subscriptions = []domain.SubscriptionSnapshot{}
		}
		patch.Subscriptions = &subscriptions
	}
	patch.Runtime = runtimeSnapshot(m.GetRuntime())
	if m.GetSkills() != nil {
		skills := skillPinSnapshots(m.GetSkills().GetValues())
		if skills == nil {
			skills = []domain.SkillPinSnapshot{}
		}
		patch.Skills = &skills
	}
	if m.GetModel() != nil {
		model := modelPolicyDefinition(m.GetModel())
		patch.Model = &model
	}
	if m.GetBudgets() != nil {
		budgets := budgetsDefinition(m.GetBudgets())
		patch.Budgets = &budgets
	}
	return patch
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
