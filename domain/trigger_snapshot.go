package domain

// TriggerSnapshot is a trigger as plain data: what a definition says and
// what the repository stores. Name defaults to the type. Objective is a
// template over the event (text/template, `{{.type}}`) for a webhook or
// a timer. A webhook authenticates with Secret (a header) or SigningKey
// (an HMAC over the body); a timer runs Every (a duration) or on Cron
// (five fields, the host's time zone).
type TriggerSnapshot struct {
	Type       string `json:"type"`
	Name       string `json:"name,omitempty"`
	Objective  string `json:"objective,omitempty"`
	Secret     string `json:"secret,omitempty"`
	SigningKey string `json:"signingKey,omitempty"`
	Every      string `json:"every,omitempty"`
	Cron       string `json:"cron,omitempty"`
}

// Redacted is the snapshot with its credentials hidden: what the API
// and the command print.
func (s TriggerSnapshot) Redacted() TriggerSnapshot {
	if s.Secret != "" {
		s.Secret = Redacted
	}
	if s.SigningKey != "" {
		s.SigningKey = Redacted
	}
	return s
}

// Redacted stands in for a credential in what is printed.
const Redacted = "<redacted>"
