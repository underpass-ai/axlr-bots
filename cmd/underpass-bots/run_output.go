package main

import "github.com/underpass-ai/underpass-bots/domain"

// runOutput is what task run and task decide print: the task after the
// run and the execution that ran it.
type runOutput struct {
	Task      domain.TaskSnapshot      `json:"task"`
	Execution domain.ExecutionSnapshot `json:"execution"`
}
