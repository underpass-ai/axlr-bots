package application

import (
	"context"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ModelProxy answers a node's model and Jev calls with the control
// plane's keys, so a device carries none. A call is taken only for an
// execution the node holds and is still running, the model is the one
// its bot names, never one the node asks for, and the bot's
// maxModelCallsPerTask is counted here. Usage stays what the node
// reports: the proxy adds none of its own, so nothing is counted twice.
type ModelProxy struct {
	deps  Dependencies
	mu    sync.Mutex
	calls map[domain.ExecutionID]int
}

// NewModelProxy wires the use case.
func NewModelProxy(deps Dependencies) *ModelProxy {
	return &ModelProxy{deps: deps, calls: map[domain.ExecutionID]int{}}
}

// Complete asks the bot's model on the execution's behalf.
func (p *ModelProxy) Complete(ctx context.Context, node domain.NodeID, id domain.ExecutionID, messages []ModelChat, tools []ModelToolSpec) (ModelAnswer, error) {
	if p.deps.ModelGateway == nil {
		return ModelAnswer{}, domain.ForbiddenError{Reason: "this control plane has no model key"}
	}
	bot, err := p.admit(ctx, node, id)
	if err != nil {
		return ModelAnswer{}, err
	}
	return p.deps.ModelGateway.Complete(ctx, ModelCall{Model: bot.ModelPolicy().Model(), Messages: messages, Tools: tools})
}

// Choose puts one choice to Jev on the execution's behalf.
func (p *ModelProxy) Choose(ctx context.Context, node domain.NodeID, id domain.ExecutionID, question JudgeQuestion) (JudgeVerdict, error) {
	if p.deps.JudgeGateway == nil {
		return JudgeVerdict{}, domain.ForbiddenError{Reason: "this control plane has no Jev key"}
	}
	if _, err := p.admit(ctx, node, id); err != nil {
		return JudgeVerdict{}, err
	}
	return p.deps.JudgeGateway.Choose(ctx, question)
}

// admit refuses a call the execution does not entitle the node to and
// counts the one it lets through.
func (p *ModelProxy) admit(ctx context.Context, node domain.NodeID, id domain.ExecutionID) (domain.Bot, error) {
	execution, err := p.deps.Executions.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	if execution.NodeID() != node {
		return domain.Bot{}, domain.ForbiddenError{Reason: "the execution is not this node's"}
	}
	if !execution.EndedAt().IsZero() || execution.LeaseExpired(p.deps.Clock.Now()) {
		return domain.Bot{}, domain.ForbiddenError{Reason: "the execution is not running"}
	}
	bot, err := p.deps.Bots.Get(ctx, execution.BotID())
	if err != nil {
		return domain.Bot{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if limit := bot.Budgets().MaxModelCallsPerTask(); limit > 0 && p.calls[id] >= limit {
		return domain.Bot{}, domain.ForbiddenError{Reason: "the execution has used its model calls"}
	}
	p.calls[id]++
	return bot, nil
}
