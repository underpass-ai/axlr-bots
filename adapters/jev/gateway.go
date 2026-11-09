package jev

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
)

// Gateway is the Jev client as the control plane's proxy reaches it.
type Gateway struct {
	Client *Client
}

// Choose asks Jev one question.
func (g Gateway) Choose(ctx context.Context, question application.JudgeQuestion) (application.JudgeVerdict, error) {
	verdict, err := g.Client.Choose(ctx, Question{State: question.State, Question: question.Question, Options: question.Options})
	if err != nil {
		return application.JudgeVerdict{}, err
	}
	return application.JudgeVerdict{Choice: verdict.Choice, Probability: verdict.Probability, Confidence: verdict.Confidence}, nil
}
