package domain

import "strings"

// Prompt is what the runtime receives for one execution: the bot's
// mission, which says what the bot is for and where its remit ends, and
// the task's objective. Without the mission in the prompt the model never
// learns the bot's role or the tools its operator prepared for it: on
// 25 October 2026 a bot whose mission named the command that reads the
// cluster answered that it had no way to, because only the objective
// reached it.
type Prompt string

// MaxPromptLength is the most a prompt may hold. It is the objective's
// limit, because a node receives the prompt in the protocol's objective
// field and validates it as one; nodes enrolled before the prompt existed
// then run it unchanged.
const MaxPromptLength = 8000

// NewPrompt composes a bot's mission and a task's objective. A mission
// and an objective that do not fit together are refused, not cut.
func NewPrompt(mission Mission, objective Objective) (Prompt, error) {
	value, err := validateText("prompt", "You are a bot. Your mission, set by your operator:\n"+mission.String()+"\n\nThe task:\n"+objective.String(), MaxPromptLength)
	if err != nil {
		return "", err
	}
	return Prompt(value), nil
}

// NewTeamPrompt composes the mission, the team directory, the notes of
// what the bot heard and the objective. The directory and the notes
// take what the mission and the objective leave of the prompt's limit:
// the notes are cut first (the oldest), then the directory (from the
// end); neither ever cuts the mission or the objective, and what does
// not fit at all is left out.
func NewTeamPrompt(mission Mission, objective Objective, directory TeamDirectory, notes EventNotes) (Prompt, error) {
	plain, err := NewPrompt(mission, objective)
	if err != nil || directory.Empty() && notes.Empty() {
		return plain, err
	}
	head := "You are a bot. Your mission, set by your operator:\n" + mission.String() + "\n\n"
	tail := "\n\nThe task:\n" + objective.String()
	room := MaxPromptLength - len(head) - len(tail)
	var blocks []string
	if rendered := directory.Render(room); rendered != "" {
		blocks = append(blocks, rendered)
		room -= len(rendered) + len("\n\n")
	}
	if rendered := notes.Render(room); rendered != "" {
		blocks = append(blocks, rendered)
	}
	if len(blocks) == 0 {
		return plain, nil
	}
	return newPromptText(head + strings.Join(blocks, "\n\n") + tail)
}

// newPromptText validates a composed prompt.
func newPromptText(text string) (Prompt, error) {
	value, err := validateText("prompt", text, MaxPromptLength)
	if err != nil {
		return "", err
	}
	return Prompt(value), nil
}

func (p Prompt) String() string { return string(p) }
