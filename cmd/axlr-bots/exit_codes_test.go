package main

import (
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// A decision a node carries answers Running: accepted, not failed.
func TestExitCodeForEachOutcome(t *testing.T) {
	cases := map[domain.ExecutionOutcome]int{
		domain.ExecutionCompleted:        exitOK,
		domain.ExecutionRunning:          exitOK,
		domain.ExecutionDecisionRequired: exitDecisionRequired,
		domain.ExecutionFailed:           exitError,
		domain.ExecutionCancelled:        exitError,
	}
	for outcome, want := range cases {
		if got := exitCodeFor(outcome.String()); got != want {
			t.Errorf("%s: exit %d, want %d", outcome, got, want)
		}
	}
}
