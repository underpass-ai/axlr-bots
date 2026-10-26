package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// Limits of a trigger's credentials and of the events it hears.
const (
	minCredentialLength = 16
	maxCredentialLength = 256
	maxTriggerEvents    = 16
)

// Trigger is one way tasks reach the bot, as its definition declares it:
// its type, a name unique within the bot (the type when not given), and
// for a webhook or a timer the objective template each fire renders and
// the credentials or the schedule.
type Trigger struct {
	kind       TriggerType
	name       string
	objective  string
	secret     string
	signingKey string
	schedule   Schedule
	events     []EventPattern
	interrupt  bool
}

// NewTrigger validates a trigger from its definition.
func NewTrigger(s TriggerSnapshot) (Trigger, error) {
	kind, err := ParseTriggerType(s.Type)
	if err != nil {
		return Trigger{}, err
	}
	name := s.Name
	if name == "" {
		name = kind.String()
	}
	name, err = validateIdentifier("trigger name", name)
	if err != nil {
		return Trigger{}, err
	}
	t := Trigger{kind: kind, name: name}
	if (len(s.Events) > 0 || s.Interrupt) && kind != TriggerEvent {
		return Trigger{}, invalid("trigger "+name, "only an event trigger takes events or interrupt")
	}
	t.interrupt = s.Interrupt
	switch kind {
	case TriggerManual, TriggerDelegation:
		if s.Objective != "" || s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "a "+kind.String()+" trigger takes no objective, credentials or schedule")
		}
		return t, nil
	case TriggerWebhook:
		if s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "a webhook takes no schedule")
		}
		if s.Secret == "" && s.SigningKey == "" {
			return Trigger{}, invalid("trigger "+name, "a webhook needs a secret or a signingKey")
		}
		if t.secret, err = validateCredential("trigger "+name+" secret", s.Secret); err != nil {
			return Trigger{}, err
		}
		if t.signingKey, err = validateCredential("trigger "+name+" signingKey", s.SigningKey); err != nil {
			return Trigger{}, err
		}
	case TriggerEvent:
		if s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "an event trigger takes no credentials or schedule")
		}
		if len(s.Events) == 0 || len(s.Events) > maxTriggerEvents {
			return Trigger{}, invalid("trigger "+name, "an event trigger names 1 to 16 events or patterns")
		}
		for _, raw := range s.Events {
			pattern, err := NewEventPattern(raw)
			if err != nil {
				return Trigger{}, invalid("trigger "+name, err.Error())
			}
			t.events = append(t.events, pattern)
		}
		if s.Objective == "" {
			return t, nil
		}
	case TriggerTimer:
		if s.Secret != "" || s.SigningKey != "" {
			return Trigger{}, invalid("trigger "+name, "a timer takes no credentials")
		}
		if t.schedule, err = NewSchedule(s.Every, s.Cron); err != nil {
			return Trigger{}, invalid("trigger "+name, err.Error())
		}
	}
	if err := ValidateObjectiveTemplate(s.Objective); err != nil {
		return Trigger{}, invalid("trigger "+name+" objective", err.Error())
	}
	t.objective = strings.TrimSpace(s.Objective)
	return t, nil
}

// validateCredential accepts an empty credential or one of 16 to 256
// printable characters without spaces.
func validateCredential(field, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) < minCredentialLength || len(raw) > maxCredentialLength {
		return "", invalid(field, "must be 16 to 256 characters")
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return "", invalid(field, "must not contain spaces or control characters")
		}
	}
	return raw, nil
}

// Type is the trigger's type.
func (t Trigger) Type() TriggerType { return t.kind }

// Name is the trigger's name, unique within the bot.
func (t Trigger) Name() string { return t.name }

// Ref names the trigger on a task.
func (t Trigger) Ref() TriggerRef { return TriggerRef{Type: t.kind, Name: t.name} }

// ObjectiveTemplate is the template each fire renders into the task's
// objective; empty for a manual or delegation trigger, and for an event
// trigger that keeps the default (DefaultEventObjective).
func (t Trigger) ObjectiveTemplate() string { return t.objective }

// Schedule is when a timer fires; zero for the other types.
func (t Trigger) Schedule() Schedule { return t.schedule }

// Authenticates reports whether a webhook request may fire the trigger:
// the presented secret equals the trigger's, or the signature header
// (`sha256=<hex>`, or the hex alone) is the HMAC-SHA256 of the body
// under the signing key. Comparisons take constant time.
func (t Trigger) Authenticates(presentedSecret, signature string, body []byte) bool {
	if t.kind != TriggerWebhook {
		return false
	}
	if t.secret != "" && presentedSecret != "" && subtle.ConstantTimeCompare([]byte(presentedSecret), []byte(t.secret)) == 1 {
		return true
	}
	if t.signingKey != "" && signature != "" {
		mac := hmac.New(sha256.New, []byte(t.signingKey))
		mac.Write(body)
		want := hex.EncodeToString(mac.Sum(nil))
		got := strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")
		if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

// TeamEventTrigger is the trigger a bot hears its team by when it
// declares no event trigger of its own.
func TeamEventTrigger() Trigger { return Trigger{kind: TriggerEvent, name: "team"} }

// Interrupts reports whether the warnings and criticals an event
// trigger hears make urgent tasks.
func (t Trigger) Interrupts() bool { return t.interrupt }

// Hears reports whether an event trigger takes the event.
func (t Trigger) Hears(name EventName) bool {
	for _, pattern := range t.events {
		if pattern.Matches(name) {
			return true
		}
	}
	return false
}

// Snapshot returns the trigger as plain data, credentials included: what
// the repository stores. Redact it before printing.
func (t Trigger) Snapshot() TriggerSnapshot {
	s := TriggerSnapshot{Type: t.kind.String(), Name: t.name, Objective: t.objective, Secret: t.secret, SigningKey: t.signingKey}
	if t.kind == TriggerTimer {
		s.Every, s.Cron = t.schedule.Snapshot()
	}
	for _, pattern := range t.events {
		s.Events = append(s.Events, pattern.String())
	}
	s.Interrupt = t.interrupt
	return s
}
