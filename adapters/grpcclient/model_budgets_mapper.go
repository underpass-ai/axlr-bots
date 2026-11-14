package grpcclient

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func modelPolicySnapshot(m *operatorv1.ModelPolicy) domain.ModelPolicySnapshot {
	return domain.ModelPolicySnapshot{Model: m.GetModel(), RoutingPolicy: m.GetRoutingPolicy(), AllowRemoteProviders: m.GetAllowRemoteProviders(), MaxContextTokens: int(m.GetMaxContextTokens())}
}

func budgetsSnapshot(b *operatorv1.Budgets) domain.BudgetsSnapshot {
	return domain.BudgetsSnapshot{
		MaxModelCallsPerTask: int(b.GetMaxModelCallsPerTask()), MaxToolCallsPerTask: int(b.GetMaxToolCallsPerTask()),
		MaxDelegationDepth: int(b.GetMaxDelegationDepth()), TaskTimeoutSeconds: seconds(b.GetTaskTimeout()),
	}
}

func modelPolicyMessage(m application.ModelPolicyDefinition) *operatorv1.ModelPolicy {
	return &operatorv1.ModelPolicy{Model: m.Model, RoutingPolicy: m.RoutingPolicy, AllowRemoteProviders: m.AllowRemoteProviders, MaxContextTokens: int32(m.MaxContextTokens)}
}

func budgetsMessage(b application.BudgetsDefinition) *operatorv1.Budgets {
	message := &operatorv1.Budgets{MaxModelCallsPerTask: int32(b.MaxModelCallsPerTask), MaxToolCallsPerTask: int32(b.MaxToolCallsPerTask), MaxDelegationDepth: int32(b.MaxDelegationDepth)}
	if b.TaskTimeoutSeconds != 0 {
		message.TaskTimeout = durationpb.New(time.Duration(b.TaskTimeoutSeconds) * time.Second)
	}
	return message
}

func modelPolicyDefinition(m *operatorv1.ModelPolicy) application.ModelPolicyDefinition {
	return application.ModelPolicyDefinition{Model: m.GetModel(), RoutingPolicy: m.GetRoutingPolicy(), AllowRemoteProviders: m.GetAllowRemoteProviders(), MaxContextTokens: int(m.GetMaxContextTokens())}
}

func budgetsDefinition(b *operatorv1.Budgets) application.BudgetsDefinition {
	return application.BudgetsDefinition{
		MaxModelCallsPerTask: int(b.GetMaxModelCallsPerTask()), MaxToolCallsPerTask: int(b.GetMaxToolCallsPerTask()),
		MaxDelegationDepth: int(b.GetMaxDelegationDepth()), TaskTimeoutSeconds: seconds(b.GetTaskTimeout()),
	}
}
