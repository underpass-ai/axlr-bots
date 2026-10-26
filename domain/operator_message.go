package domain

// MaxOperatorMessageLength bounds what the operator writes to a running
// task: it goes into the model's next request.
const MaxOperatorMessageLength = 4000

// OperatorMessage is what the operator writes to a task while it runs.
type OperatorMessage string

// NewOperatorMessage validates the text: trimmed, not empty, printable,
// at most MaxOperatorMessageLength characters.
func NewOperatorMessage(raw string) (OperatorMessage, error) {
	value, err := validateText("message", raw, MaxOperatorMessageLength)
	if err != nil {
		return "", err
	}
	return OperatorMessage(value), nil
}

func (m OperatorMessage) String() string { return string(m) }
