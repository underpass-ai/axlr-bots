package domain

// Objective is what a task asks the bot to achieve, in the words the
// runtime receives as its prompt.
type Objective string

// MaxObjectiveLength bounds an objective, in bytes.
const MaxObjectiveLength = 8000

// NewObjective validates an objective: 1 to 8000 characters of text.
func NewObjective(raw string) (Objective, error) {
	value, err := validateText("objective", raw, MaxObjectiveLength)
	if err != nil {
		return "", err
	}
	return Objective(value), nil
}

func (o Objective) String() string { return string(o) }
