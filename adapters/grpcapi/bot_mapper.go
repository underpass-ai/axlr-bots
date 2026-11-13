package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// botToProto is a bot as the operator API shows it; the snapshot comes
// redacted from the service.
func botToProto(s domain.BotSnapshot) *operatorv1.Bot {
	bot := &operatorv1.Bot{
		Id: s.ID, TenantId: s.TenantID, Name: s.Name, Owner: s.Owner, Mission: s.Mission, AllowedTools: s.AllowedTools,
		Model: modelPolicyToProto(s.Model), Budgets: budgetsToProto(s.Budgets), Level: s.Level,
		ToolRules: toolRulesToProto(s.ToolRules), Triggers: triggersToProto(s.Triggers), Placement: placementToProto(s.Placement),
		Approvals: approvalsToProto(s.Approvals), Delegation: delegationToProto(s.Delegation), Groups: s.Groups,
		Events: eventsToProto(s.Events), Subscriptions: subscriptionsToProto(s.Subscriptions), Runtime: runtimeToProto(s.Runtime),
		Skills: skillsToProto(s.Skills), Version: s.Version, Status: s.Status,
		CreatedAt: timestamp(s.CreatedAt), UpdatedAt: timestamp(s.UpdatedAt),
	}
	if s.Staged != nil {
		bot.Staged = &operatorv1.StagedDefinition{Version: s.Staged.Version, StagedAt: timestamp(s.Staged.StagedAt), Change: s.Staged.Change}
	}
	return bot
}

func botsToProto(snapshots []domain.BotSnapshot) []*operatorv1.Bot {
	bots := make([]*operatorv1.Bot, 0, len(snapshots))
	for _, s := range snapshots {
		bots = append(bots, botToProto(s))
	}
	return bots
}

func modelPolicyToProto(m domain.ModelPolicySnapshot) *operatorv1.ModelPolicy {
	return &operatorv1.ModelPolicy{Model: m.Model, RoutingPolicy: m.RoutingPolicy, AllowRemoteProviders: m.AllowRemoteProviders, MaxContextTokens: int32(m.MaxContextTokens)}
}

func modelPolicyDefinitionToProto(m application.ModelPolicyDefinition) *operatorv1.ModelPolicy {
	return &operatorv1.ModelPolicy{Model: m.Model, RoutingPolicy: m.RoutingPolicy, AllowRemoteProviders: m.AllowRemoteProviders, MaxContextTokens: int32(m.MaxContextTokens)}
}

func budgetsToProto(b domain.BudgetsSnapshot) *operatorv1.Budgets {
	return &operatorv1.Budgets{
		MaxModelCallsPerTask: int32(b.MaxModelCallsPerTask), MaxToolCallsPerTask: int32(b.MaxToolCallsPerTask),
		MaxDelegationDepth: int32(b.MaxDelegationDepth), TaskTimeout: seconds(b.TaskTimeoutSeconds),
	}
}

func budgetsDefinitionToProto(b application.BudgetsDefinition) *operatorv1.Budgets {
	return &operatorv1.Budgets{
		MaxModelCallsPerTask: int32(b.MaxModelCallsPerTask), MaxToolCallsPerTask: int32(b.MaxToolCallsPerTask),
		MaxDelegationDepth: int32(b.MaxDelegationDepth), TaskTimeout: seconds(b.TaskTimeoutSeconds),
	}
}

func toolRuleListToProto(rules []domain.ToolRuleSnapshot) []*operatorv1.ToolRule {
	out := make([]*operatorv1.ToolRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, &operatorv1.ToolRule{Tool: r.Tool, Arguments: r.Arguments})
	}
	return out
}

func toolRulesToProto(r domain.ToolRulesSnapshot) *operatorv1.ToolRules {
	return &operatorv1.ToolRules{Allow: toolRuleListToProto(r.Allow), Deny: toolRuleListToProto(r.Deny)}
}

func triggersToProto(triggers []domain.TriggerSnapshot) []*operatorv1.Trigger {
	out := make([]*operatorv1.Trigger, 0, len(triggers))
	for _, t := range triggers {
		out = append(out, &operatorv1.Trigger{
			Type: t.Type, Name: t.Name, Objective: t.Objective, Secret: t.Secret, SigningKey: t.SigningKey,
			Every: durationText(t.Every), Cron: t.Cron, Events: t.Events, Interrupt: t.Interrupt,
		})
	}
	return out
}

func placementToProto(p domain.PlacementSnapshot) *operatorv1.Placement {
	return &operatorv1.Placement{Nodes: p.Nodes, Selector: p.Selector}
}

func approvalsToProto(a domain.ApprovalsSnapshot) *operatorv1.Approvals {
	notify := make([]*operatorv1.NotifyTarget, 0, len(a.Notify))
	for _, n := range a.Notify {
		notify = append(notify, &operatorv1.NotifyTarget{Type: n.Type, Name: n.Name, Url: n.URL, Secret: n.Secret, Command: n.Command})
	}
	return &operatorv1.Approvals{Ttl: durationText(a.TTL), OnExpire: a.OnExpire, Notify: notify, AutoApprove: toolRuleListToProto(a.AutoApprove), Approvers: a.Approvers}
}

func delegationToProto(d *domain.DelegationSnapshot) *operatorv1.Delegation {
	if d == nil {
		return nil
	}
	return &operatorv1.Delegation{To: d.To, MaxDepth: int32(d.MaxDepth), BudgetShare: d.BudgetShare, Split: d.Split, MaxChildren: int32(d.MaxChildren), FromTeam: d.FromTeam}
}

func eventsToProto(e *domain.EventsSnapshot) *operatorv1.Events {
	if e == nil {
		return nil
	}
	emit := make([]*operatorv1.EmitRule, 0, len(e.Emit))
	for _, r := range e.Emit {
		emit = append(emit, &operatorv1.EmitRule{Name: r.Name, MaxSeverity: r.MaxSeverity})
	}
	return &operatorv1.Events{Emit: emit, PerMinute: int32(e.PerMinute), IntakePerMinute: int32(e.IntakePerMinute), MaxDepth: int32(e.MaxDepth), Interruptible: e.Interruptible}
}

func subscriptionsToProto(subscriptions []domain.SubscriptionSnapshot) []*operatorv1.Subscription {
	out := make([]*operatorv1.Subscription, 0, len(subscriptions))
	for _, s := range subscriptions {
		out = append(out, &operatorv1.Subscription{
			Topic: s.Topic, Mode: s.Mode, From: s.From, Urgent: s.Urgent, Objective: s.Objective, Status: s.Status,
			AskedBy: s.AskedBy, Why: s.Why, AskedAt: timestampOf(s.AskedAt),
		})
	}
	return out
}

func runtimeToProto(r *domain.RuntimeSnapshot) *operatorv1.Runtime {
	if r == nil {
		return nil
	}
	runtime := &operatorv1.Runtime{Kind: r.Kind}
	if j := r.Jev; j != nil {
		program := &operatorv1.JevProgram{
			Server: j.Server, Start: j.Start, Wait: j.Wait, Stop: j.Stop, Model: j.Model,
			Act: &operatorv1.JevAct{Tool: j.Act.Tool, Choice: j.Act.Choice, Verdict: j.Act.Verdict, Pass: j.Act.Pass},
		}
		if j.Tell != nil {
			program.Tell = &operatorv1.JevTell{Name: j.Tell.Name, Fields: j.Tell.Fields}
		}
		runtime.Jev = program
	}
	return runtime
}

func skillsToProto(skills []domain.SkillPinSnapshot) []*operatorv1.SkillPin {
	out := make([]*operatorv1.SkillPin, 0, len(skills))
	for _, s := range skills {
		out = append(out, &operatorv1.SkillPin{Name: s.Name, Version: s.Version, Catalog: s.Catalog})
	}
	return out
}

// patchToProto is a staged patch as GetStagedBot answers it: a field the
// patch leaves alone stays unset.
func patchToProto(p application.BotPatch) *operatorv1.BotPatch {
	patch := &operatorv1.BotPatch{Version: p.Version, Mission: p.Mission, Level: p.Level}
	if p.AllowedTools != nil {
		patch.AllowedTools = &operatorv1.StringList{Values: *p.AllowedTools}
	}
	if p.ToolRules != nil {
		patch.ToolRules = toolRulesToProto(*p.ToolRules)
	}
	if p.Triggers != nil {
		patch.Triggers = &operatorv1.TriggerList{Values: triggersToProto(*p.Triggers)}
	}
	if p.Placement != nil {
		patch.Placement = placementToProto(*p.Placement)
	}
	if p.Approvals != nil {
		patch.Approvals = approvalsToProto(*p.Approvals)
	}
	if p.AutoApprove != nil {
		patch.AutoApprove = &operatorv1.ToolRuleList{Values: toolRuleListToProto(*p.AutoApprove)}
	}
	patch.Delegation = delegationToProto(p.Delegation)
	if p.Groups != nil {
		patch.Groups = &operatorv1.StringList{Values: *p.Groups}
	}
	patch.Events = eventsToProto(p.Events)
	if p.Subscriptions != nil {
		patch.Subscriptions = &operatorv1.SubscriptionList{Values: subscriptionsToProto(*p.Subscriptions)}
	}
	patch.Runtime = runtimeToProto(p.Runtime)
	if p.Skills != nil {
		patch.Skills = &operatorv1.SkillPinList{Values: skillsToProto(*p.Skills)}
	}
	if p.Model != nil {
		patch.Model = modelPolicyDefinitionToProto(*p.Model)
	}
	if p.Budgets != nil {
		patch.Budgets = budgetsDefinitionToProto(*p.Budgets)
	}
	return patch
}
