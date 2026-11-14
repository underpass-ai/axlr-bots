package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
)

// botDefinitionMessage is what RegisterBot sends for a definition read
// from a bot file.
func botDefinitionMessage(d application.BotDefinition) (*operatorv1.BotDefinition, error) {
	triggers, err := triggerMessages(d.Triggers)
	if err != nil {
		return nil, err
	}
	approvals, err := approvalsMessage(d.Approvals)
	if err != nil {
		return nil, err
	}
	return &operatorv1.BotDefinition{
		Id: d.ID, Version: d.Version, Tenant: d.Tenant, Name: d.Name, Owner: d.Owner, Mission: d.Mission,
		AllowedTools: d.AllowedTools, Level: d.Level, ToolRules: toolRulesMessage(d.ToolRules), Triggers: triggers,
		Placement: placementMessage(d.Placement), Approvals: approvals, Delegation: delegationMessage(d.Delegation),
		Groups: d.Groups, Events: botEventsMessage(d.Events), Subscriptions: subscriptionMessages(d.Subscriptions),
		Runtime: runtimeMessage(d.Runtime), Skills: skillPinMessages(d.Skills),
		Model: modelPolicyMessage(d.Model), Budgets: budgetsMessage(d.Budgets),
	}, nil
}
