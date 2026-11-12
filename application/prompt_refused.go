package application

import (
	"fmt"

	"github.com/underpass-ai/axlr-bots/domain"
)

// errPromptRefused says a task cannot be handed to a runtime: its bot's
// mission and its objective do not make a prompt. Its text is the reason
// the task fails with, sizes only: an operator reads it on the task and
// on the incident, and a mission or an objective may be long or private.
type errPromptRefused struct {
	cause              error
	mission, objective int
}

func (e errPromptRefused) Error() string {
	return fmt.Sprintf("prompt refused: %v (mission %d bytes, objective %d bytes, prompt limit %d bytes)", e.cause, e.mission, e.objective, domain.MaxPromptLength)
}

func (e errPromptRefused) Unwrap() error { return e.cause }

// composedPrompt composes the prompt of the task's run, or the refusal
// that ends the task.
func composedPrompt(bot domain.Bot, task domain.Task, mates []domain.Bot) (domain.Prompt, error) {
	reserve := domain.ContextRoom(contextRefsOf(task), memoryTeamOf(bot))
	prompt, err := domain.NewTeamPromptLeaving(bot.Mission(), task.Objective(), directoryOf(bot, mates), task.Notes(), reserve)
	if err != nil {
		return "", errPromptRefused{cause: err, mission: len(bot.Mission().String()), objective: len(task.Objective().String())}
	}
	return prompt, nil
}
