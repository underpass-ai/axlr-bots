package domain

// NotifyKind is how a notification reaches a person: an HTTP POST to a
// webhook, or a local program that reads the notification on stdin.
type NotifyKind string

const (
	// NotifyWebhook posts the notification as JSON, signed.
	NotifyWebhook NotifyKind = "webhook"
	// NotifyCommand runs a program with the notification on its stdin.
	NotifyCommand NotifyKind = "command"
)

// ParseNotifyKind validates a notification target's type.
func ParseNotifyKind(raw string) (NotifyKind, error) {
	switch NotifyKind(raw) {
	case NotifyWebhook, NotifyCommand:
		return NotifyKind(raw), nil
	}
	return "", invalid("notify type", "must be webhook or command")
}

func (k NotifyKind) String() string { return string(k) }
