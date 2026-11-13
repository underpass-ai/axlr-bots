package grpcapi

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// definitionFromProto is the definition RegisterBot takes, as the use
// case wants it.
func definitionFromProto(d *operatorv1.BotDefinition) application.BotDefinition {
	return application.BotDefinition{
		ID: d.GetId(), Version: d.GetVersion(), Tenant: d.GetTenant(), Name: d.GetName(), Owner: d.GetOwner(), Mission: d.GetMission(),
		AllowedTools: d.GetAllowedTools(), Level: d.GetLevel(), ToolRules: toolRulesFromProto(d.GetToolRules()),
		Triggers: triggersFromProto(d.GetTriggers()), Placement: placementFromProto(d.GetPlacement()), Approvals: approvalsFromProto(d.GetApprovals()),
		Delegation: delegationFromProto(d.GetDelegation()), Groups: d.GetGroups(), Events: eventsFromProto(d.GetEvents()),
		Subscriptions: subscriptionsFromProto(d.GetSubscriptions()), Runtime: runtimeFromProto(d.GetRuntime()), Skills: skillsFromProto(d.GetSkills()),
		Model: modelPolicyFromProto(d.GetModel()), Budgets: budgetsFromProto(d.GetBudgets()),
	}
}

// patchFromProto is the partial change UpdateBot and StageBot take: what
// is unset stays nil, an empty list wrapper is a list to clear.
func patchFromProto(p *operatorv1.BotPatch) application.BotPatch {
	if p == nil {
		p = &operatorv1.BotPatch{}
	}
	patch := application.BotPatch{Version: p.Version, Mission: p.Mission, Level: p.Level}
	if p.GetAllowedTools() != nil {
		values := nonNil(p.GetAllowedTools().GetValues())
		patch.AllowedTools = &values
	}
	if p.GetToolRules() != nil {
		rules := toolRulesFromProto(p.GetToolRules())
		patch.ToolRules = &rules
	}
	if p.GetTriggers() != nil {
		values := triggersFromProto(p.GetTriggers().GetValues())
		patch.Triggers = &values
	}
	if p.GetPlacement() != nil {
		placement := placementFromProto(p.GetPlacement())
		patch.Placement = &placement
	}
	if p.GetApprovals() != nil {
		approvals := approvalsFromProto(p.GetApprovals())
		patch.Approvals = &approvals
	}
	if p.GetAutoApprove() != nil {
		rules := application.AutoApproveRules(toolRuleListFromProto(p.GetAutoApprove().GetValues()))
		patch.AutoApprove = &rules
	}
	patch.Delegation = delegationFromProto(p.GetDelegation())
	if p.GetGroups() != nil {
		values := nonNil(p.GetGroups().GetValues())
		patch.Groups = &values
	}
	patch.Events = eventsFromProto(p.GetEvents())
	if p.GetSubscriptions() != nil {
		values := subscriptionsFromProto(p.GetSubscriptions().GetValues())
		patch.Subscriptions = &values
	}
	patch.Runtime = runtimeFromProto(p.GetRuntime())
	if p.GetSkills() != nil {
		values := skillsFromProto(p.GetSkills().GetValues())
		patch.Skills = &values
	}
	if p.GetModel() != nil {
		model := modelPolicyFromProto(p.GetModel())
		patch.Model = &model
	}
	if p.GetBudgets() != nil {
		budgets := budgetsFromProto(p.GetBudgets())
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

func modelPolicyFromProto(m *operatorv1.ModelPolicy) application.ModelPolicyDefinition {
	return application.ModelPolicyDefinition{Model: m.GetModel(), RoutingPolicy: m.GetRoutingPolicy(), AllowRemoteProviders: m.GetAllowRemoteProviders(), MaxContextTokens: int(m.GetMaxContextTokens())}
}

func budgetsFromProto(b *operatorv1.Budgets) application.BudgetsDefinition {
	return application.BudgetsDefinition{
		MaxModelCallsPerTask: int(b.GetMaxModelCallsPerTask()), MaxToolCallsPerTask: int(b.GetMaxToolCallsPerTask()),
		MaxDelegationDepth: int(b.GetMaxDelegationDepth()), TaskTimeoutSeconds: int(b.GetTaskTimeout().AsDuration().Seconds()),
	}
}

func toolRuleListFromProto(rules []*operatorv1.ToolRule) []domain.ToolRuleSnapshot {
	if rules == nil {
		return nil
	}
	out := make([]domain.ToolRuleSnapshot, 0, len(rules))
	for _, r := range rules {
		out = append(out, domain.ToolRuleSnapshot{Tool: r.GetTool(), Arguments: r.GetArguments()})
	}
	return out
}

func toolRulesFromProto(r *operatorv1.ToolRules) domain.ToolRulesSnapshot {
	return domain.ToolRulesSnapshot{Allow: toolRuleListFromProto(r.GetAllow()), Deny: toolRuleListFromProto(r.GetDeny())}
}

func triggersFromProto(triggers []*operatorv1.Trigger) []domain.TriggerSnapshot {
	if triggers == nil {
		return nil
	}
	out := make([]domain.TriggerSnapshot, 0, len(triggers))
	for _, t := range triggers {
		out = append(out, domain.TriggerSnapshot{
			Type: t.GetType(), Name: t.GetName(), Objective: t.GetObjective(), Secret: t.GetSecret(), SigningKey: t.GetSigningKey(),
			Every: textOf(t.GetEvery()), Cron: t.GetCron(), Events: t.GetEvents(), Interrupt: t.GetInterrupt(),
		})
	}
	return out
}

func placementFromProto(p *operatorv1.Placement) domain.PlacementSnapshot {
	return domain.PlacementSnapshot{Nodes: p.GetNodes(), Selector: p.GetSelector()}
}

func approvalsFromProto(a *operatorv1.Approvals) domain.ApprovalsSnapshot {
	var notify []domain.NotifyTargetSnapshot
	for _, n := range a.GetNotify() {
		notify = append(notify, domain.NotifyTargetSnapshot{Type: n.GetType(), Name: n.GetName(), URL: n.GetUrl(), Secret: n.GetSecret(), Command: n.GetCommand()})
	}
	return domain.ApprovalsSnapshot{
		TTL: textOf(a.GetTtl()), OnExpire: a.GetOnExpire(), Notify: notify,
		AutoApprove: toolRuleListFromProto(a.GetAutoApprove()), Approvers: a.GetApprovers(),
	}
}

func delegationFromProto(d *operatorv1.Delegation) *domain.DelegationSnapshot {
	if d == nil {
		return nil
	}
	return &domain.DelegationSnapshot{To: d.GetTo(), MaxDepth: int(d.GetMaxDepth()), BudgetShare: d.GetBudgetShare(), Split: d.GetSplit(), MaxChildren: int(d.GetMaxChildren()), FromTeam: d.FromTeam}
}

func eventsFromProto(e *operatorv1.Events) *domain.EventsSnapshot {
	if e == nil {
		return nil
	}
	var emit []domain.EmitRuleSnapshot
	for _, r := range e.GetEmit() {
		emit = append(emit, domain.EmitRuleSnapshot{Name: r.GetName(), MaxSeverity: r.GetMaxSeverity()})
	}
	return &domain.EventsSnapshot{Emit: emit, PerMinute: int(e.GetPerMinute()), IntakePerMinute: int(e.GetIntakePerMinute()), MaxDepth: int(e.GetMaxDepth()), Interruptible: e.Interruptible}
}

func subscriptionsFromProto(subscriptions []*operatorv1.Subscription) []domain.SubscriptionSnapshot {
	if subscriptions == nil {
		return nil
	}
	out := make([]domain.SubscriptionSnapshot, 0, len(subscriptions))
	for _, s := range subscriptions {
		snapshot := domain.SubscriptionSnapshot{
			Topic: s.GetTopic(), Mode: s.GetMode(), From: s.GetFrom(), Urgent: s.GetUrgent(), Objective: s.GetObjective(),
			Status: s.GetStatus(), AskedBy: s.GetAskedBy(), Why: s.GetWhy(),
		}
		if s.GetAskedAt() != nil {
			at := instant(s.GetAskedAt())
			snapshot.AskedAt = &at
		}
		out = append(out, snapshot)
	}
	return out
}

func runtimeFromProto(r *operatorv1.Runtime) *domain.RuntimeSnapshot {
	if r == nil {
		return nil
	}
	runtime := &domain.RuntimeSnapshot{Kind: r.GetKind()}
	if j := r.GetJev(); j != nil {
		program := &domain.JevProgramSnapshot{
			Server: j.GetServer(), Start: j.GetStart(), Wait: j.GetWait(), Stop: j.GetStop(), Model: j.GetModel(),
			Act: domain.JevActSnapshot{Tool: j.GetAct().GetTool(), Choice: j.GetAct().GetChoice(), Verdict: j.GetAct().GetVerdict(), Pass: j.GetAct().GetPass()},
		}
		if j.GetTell() != nil {
			program.Tell = &domain.JevTellSnapshot{Name: j.GetTell().GetName(), Fields: j.GetTell().GetFields()}
		}
		runtime.Jev = program
	}
	return runtime
}

func skillsFromProto(skills []*operatorv1.SkillPin) []domain.SkillPinSnapshot {
	if skills == nil {
		return nil
	}
	out := make([]domain.SkillPinSnapshot, 0, len(skills))
	for _, s := range skills {
		out = append(out, domain.SkillPinSnapshot{Name: s.GetName(), Version: s.GetVersion(), Catalog: s.GetCatalog()})
	}
	return out
}
