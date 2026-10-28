package domain

// DeliveryMode is what a heard event does to the bot that hears it.
type DeliveryMode string

const (
	// ModeTask makes a new task of the bot.
	ModeTask DeliveryMode = "task"
	// ModeMessage goes into the bot's running task as a teammate's line;
	// a new task when it has none.
	ModeMessage DeliveryMode = "message"
	// ModeNote is kept and put in the prompt of the bot's next task.
	ModeNote DeliveryMode = "note"
)

// ParseDeliveryMode validates a mode; empty means "by the severity".
func ParseDeliveryMode(raw string) (DeliveryMode, error) {
	switch mode := DeliveryMode(raw); mode {
	case "", ModeTask, ModeMessage, ModeNote:
		return mode, nil
	}
	return "", invalid("subscription mode", "must be task, message or note")
}

// DefaultMode is the mode of a subscription that names none: info is a
// note, a warning a message, a critical a task.
func DefaultMode(severity Severity) DeliveryMode {
	switch severity {
	case SeverityCritical:
		return ModeTask
	case SeverityWarning:
		return ModeMessage
	}
	return ModeNote
}

func (m DeliveryMode) String() string { return string(m) }
