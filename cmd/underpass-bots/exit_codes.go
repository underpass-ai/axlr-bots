package main

// Exit codes of the command. task run mirrors the execution's outcome so
// a script can branch on it.
const (
	exitOK               = 0
	exitError            = 1
	exitUsage            = 2
	exitDecisionRequired = 3
)
