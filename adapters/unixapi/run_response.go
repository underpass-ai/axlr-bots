package unixapi

import "github.com/underpass-ai/underpass-bots/domain"

// RunResponse is what a run or a decision returns: the task after it and
// the execution that carried it.
type RunResponse struct {
	Task      domain.TaskSnapshot      `json:"task"`
	Execution domain.ExecutionSnapshot `json:"execution"`
}
