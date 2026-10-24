package main

import "github.com/underpass-ai/underpass-bots/domain"

// Exit codes of the command. task run and task decide mirror the
// execution's outcome so a script can branch on it.
const (
	exitOK               = 0
	exitError            = 1
	exitUsage            = 2
	exitDecisionRequired = 3
)

// exitCodeFor maps how an execution ended to the command's exit code.
func exitCodeFor(outcome domain.ExecutionOutcome) int {
	switch outcome {
	case domain.ExecutionCompleted:
		return exitOK
	case domain.ExecutionDecisionRequired:
		return exitDecisionRequired
	}
	return exitError
}
