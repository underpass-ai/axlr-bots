package main

import "github.com/underpass-ai/underpass-bots/domain"

// submitOutput is what task submit prints: whether this call created the
// task, and the task the key names.
type submitOutput struct {
	Created bool                `json:"created"`
	Task    domain.TaskSnapshot `json:"task"`
}
