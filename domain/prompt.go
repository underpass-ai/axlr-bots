package domain

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

// NewTeamPrompt composes the mission, the team directory and the
// objective. The directory takes what the mission and the objective
// leave of the prompt's limit and is cut from the end to fit; it never
// cuts them, and a directory that does not fit at all is left out.
func NewTeamPrompt(mission Mission, objective Objective, directory TeamDirectory) (Prompt, error) {
	plain, err := NewPrompt(mission, objective)
	if err != nil || directory.Empty() {
		return plain, err
	}
	head := "You are a bot. Your mission, set by your operator:\n" + mission.String() + "\n\n"
	tail := "\n\nThe task:\n" + objective.String()
	rendered := directory.Render(MaxPromptLength - len(head) - len(tail))
	if rendered == "" {
		return plain, nil
	}
	return newPromptText(head + rendered + tail)
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
