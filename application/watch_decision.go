package application

import "github.com/underpass-ai/axlr-bots/domain"

// WatchDecision is what a team's Jev watcher decided for one question:
// Jev's choice, wait or abandon, with its probability and confidence.
type WatchDecision struct {
	Question    domain.TaskID
	Choice      string
	Probability float64
	Confidence  float64
}

// validate refuses a choice that is neither wait nor abandon, and a
// verdict out of [0, 1].
func (d WatchDecision) validate() error {
	if d.Choice != domain.WatchWait && d.Choice != domain.WatchAbandon {
		return domain.ValidationError{Field: "choice", Reason: "is " + domain.WatchWait + " or " + domain.WatchAbandon}
	}
	if d.Probability < 0 || d.Probability > 1 || d.Confidence < 0 || d.Confidence > 1 {
		return domain.ValidationError{Field: "verdict", Reason: "probability and confidence are between 0 and 1"}
	}
	return nil
}
