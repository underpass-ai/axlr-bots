package application

import (
	"context"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

type recordingModels struct{ calls []ModelCall }

func (r *recordingModels) Complete(_ context.Context, call ModelCall) (ModelAnswer, error) {
	r.calls = append(r.calls, call)
	return ModelAnswer{Text: "done"}, nil
}

type stubJudge struct{}

func (stubJudge) Choose(context.Context, JudgeQuestion) (JudgeVerdict, error) {
	return JudgeVerdict{Choice: "water"}, nil
}

func TestModelProxyAsksTheBotsModelForTheNodeHoldingTheExecution(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	models := &recordingModels{}
	h.deps.ModelGateway = models
	h.deps.JudgeGateway = stubJudge{}
	proxy := NewModelProxy(h.deps)
	answer, err := proxy.Complete(ctx, node.ID(), execution, []ModelChat{{Role: "user", Content: "hi"}}, nil)
	if err != nil || answer.Text != "done" {
		t.Fatalf("complete: %v %+v", err, answer)
	}
	stored, _ := h.deps.Executions.Get(ctx, execution)
	bot, _ := h.deps.Bots.Get(ctx, stored.BotID())
	if len(models.calls) != 1 || models.calls[0].Model != bot.ModelPolicy().Model() {
		t.Errorf("calls = %+v, bot's model %q", models.calls, bot.ModelPolicy().Model())
	}
	if verdict, err := proxy.Choose(ctx, node.ID(), execution, JudgeQuestion{}); err != nil || verdict.Choice != "water" {
		t.Errorf("choose: %v %+v", err, verdict)
	}
}

func TestModelProxyRefusesWhatTheExecutionDoesNotEntitle(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	if _, err := NewModelProxy(h.deps).Complete(ctx, node.ID(), execution, nil, nil); !domain.IsForbidden(err) {
		t.Errorf("no gateway: %v", err)
	}
	models := &recordingModels{}
	h.deps.ModelGateway = models
	proxy := NewModelProxy(h.deps)
	if _, err := proxy.Complete(ctx, "another-node", execution, nil, nil); !domain.IsForbidden(err) {
		t.Errorf("another node: %v", err)
	}
	if _, err := proxy.Complete(ctx, node.ID(), "exec-ghost", nil, nil); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	h.clock.now = h.clock.now.Add(LeaseDuration + time.Minute)
	if _, err := proxy.Complete(ctx, node.ID(), execution, nil, nil); !domain.IsForbidden(err) {
		t.Errorf("expired lease: %v", err)
	}
	if len(models.calls) != 0 {
		t.Errorf("a refused call reached the model: %+v", models.calls)
	}
}

func TestModelProxyStopsAtTheBotsModelCalls(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	h.deps.ModelGateway = &recordingModels{}
	stored, _ := h.deps.Executions.Get(ctx, execution)
	bot, _ := h.deps.Bots.Get(ctx, stored.BotID())
	limit := bot.Budgets().MaxModelCallsPerTask()
	if limit == 0 {
		t.Fatal("the test bot needs a model-call budget")
	}
	proxy := NewModelProxy(h.deps)
	for i := 0; i < limit; i++ {
		if _, err := proxy.Complete(ctx, node.ID(), execution, nil, nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := proxy.Complete(ctx, node.ID(), execution, nil, nil); !domain.IsForbidden(err) {
		t.Errorf("over budget: %v", err)
	}
}
