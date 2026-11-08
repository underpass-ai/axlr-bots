package domain

// PinnedPolicySnapshot is the level and the allow-list a task's incident
// phase narrowed its bot's policy to, as plain data: kept on the task
// when it was submitted, so its runs and decisions never take the bot's
// wider policy if the trigger changes meanwhile.
type PinnedPolicySnapshot struct {
	Level        string   `json:"level"`
	AllowedTools []string `json:"allowedTools"`
}
