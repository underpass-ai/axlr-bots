package main

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/jev"
	"github.com/underpass-ai/axlr-bots/adapters/transcriptaudit"
)

// jevJudge is the transcript audit's judge over Jev's client.
type jevJudge struct {
	client jev.Judge
}

// Choose asks Jev and answers the probability of hidden_error: Jev gives
// the chosen option's, and there are two.
func (j jevJudge) Choose(ctx context.Context, choice transcriptaudit.Choice) (transcriptaudit.Judgement, error) {
	verdict, err := j.client.Choose(ctx, jev.Question{State: choice.State, Question: choice.Question, Options: choice.Options})
	if err != nil {
		return transcriptaudit.Judgement{}, err
	}
	probability := verdict.Probability
	if verdict.Choice != transcriptaudit.OptionHiddenError {
		probability = 1 - probability
	}
	return transcriptaudit.Judgement{Choice: verdict.Choice, Probability: probability, InputTokens: verdict.InputTokens}, nil
}
