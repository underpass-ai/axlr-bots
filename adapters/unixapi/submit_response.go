package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// SubmitResponse is what a submission returns: whether this call created
// the task, and the task the key names.
type SubmitResponse struct {
	Created bool                `json:"created"`
	Task    domain.TaskSnapshot `json:"task"`
}
