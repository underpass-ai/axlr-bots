package main

import "github.com/underpass-ai/axlr-bots/domain"

// Exit codes of the command. task run and task decide mirror the
// execution's outcome so a script can branch on it.
const (
	exitOK               = 0
	exitError            = 1
	exitUsage            = 2
	exitDecisionRequired = 3
)

// exitCodeFor maps how an execution ended to the command's exit code.
// A decision the daemon hands to the node that holds the session comes
// back Running: it was accepted and its end comes later, on the task's
// timeline, so it is no error (QA 5 Nov 2026: a script read every
// approval carried by a node as a failure).
func exitCodeFor(outcome string) int {
	switch outcome {
	case domain.ExecutionCompleted.String(), domain.ExecutionRunning.String():
		return exitOK
	case domain.ExecutionDecisionRequired.String():
		return exitDecisionRequired
	}
	return exitError
}
