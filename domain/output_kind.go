package domain

// OutputKind is how an incident output reaches its place: a signed JSON
// POST to a webhook, a Block Kit message posted to a chat's incoming
// webhook, or a local program that reads the JSON on its stdin.
type OutputKind string

const (
	// OutputWebhook posts the incident event as JSON, signed with the
	// target's secret.
	OutputWebhook OutputKind = "webhook"
	// OutputSlack posts a Block Kit message to an incoming-webhook URL;
	// the URL is the credential, so nothing is signed.
	OutputSlack OutputKind = "slack"
	// OutputCommand runs a program with the incident event on its stdin:
	// the relay to Teams or anything else.
	OutputCommand OutputKind = "command"
)

// ParseOutputKind validates an output's type.
func ParseOutputKind(raw string) (OutputKind, error) {
	switch OutputKind(raw) {
	case OutputWebhook, OutputSlack, OutputCommand:
		return OutputKind(raw), nil
	}
	return "", invalid("output type", "must be webhook, slack or command")
}

func (k OutputKind) String() string { return string(k) }
