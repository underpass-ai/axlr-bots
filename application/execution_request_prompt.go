package application

// PromptText is the text the runtime receives: the prompt when the
// control plane composed one (the bot's mission and the task's
// objective), the bare objective otherwise, as a node receives it from a
// control plane that does not compose prompts yet.
func (r ExecutionRequest) PromptText() string {
	if r.Prompt != "" {
		return r.Prompt.String()
	}
	return r.Objective.String()
}
