package domain

// TriggerSnapshot is a trigger as plain data: what a definition says and
// what the repository stores. Name defaults to the type. Objective is a
// template over the event (text/template, `{{.type}}`) for a webhook or
// a timer. A webhook authenticates with Secret (a header) or SigningKey
// (an HMAC over the body); a timer runs Every (a duration) or on Cron
// (five fields, the host's time zone). An event trigger hears the
// event names or patterns Events lists and, with Interrupt, makes their
// warnings and criticals urgent. An incident trigger's Phase is the
// incident's phase the bot takes: triage (the default), fix, apply or
// verify; its Level and Tools, when given, are the level and the
// allow-list that phase's tasks run under: at most the bot's level, and
// a subset of its allowedTools (Bot.CheckTriggers refuses more). A
// cloudwatch trigger's Severity is its incidents' (sev3 when absent) and
// AutoConfirm says whether it confirms an SNS subscription by itself.
type TriggerSnapshot struct {
	Type        string   `json:"type"`
	Name        string   `json:"name,omitempty"`
	Objective   string   `json:"objective,omitempty"`
	Secret      string   `json:"secret,omitempty"`
	SigningKey  string   `json:"signingKey,omitempty"`
	Every       string   `json:"every,omitempty"`
	Cron        string   `json:"cron,omitempty"`
	Events      []string `json:"events,omitempty"`
	Interrupt   bool     `json:"interrupt,omitempty"`
	Phase       string   `json:"phase,omitempty"`
	Level       string   `json:"level,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Severity    string   `json:"severity,omitempty"`
	AutoConfirm bool     `json:"autoConfirm,omitempty"`
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
