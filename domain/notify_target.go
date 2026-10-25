package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"strings"
)

// Limits of a notification target.
const (
	maxNotifyURLLength = 2048
	maxCommandArgs     = 32
	maxCommandArgBytes = 1024
)

// NotifyTarget is one place a bot's approval requests go: a webhook,
// whose deliveries are signed with its secret, or a local command.
type NotifyTarget struct {
	kind    NotifyKind
	name    string
	url     string
	secret  string
	command []string
}

// NewNotifyTarget validates a target from its definition.
func NewNotifyTarget(s NotifyTargetSnapshot) (NotifyTarget, error) {
	kind, err := ParseNotifyKind(s.Type)
	if err != nil {
		return NotifyTarget{}, err
	}
	name := s.Name
	if name == "" {
		name = kind.String()
	}
	if name, err = validateIdentifier("notify name", name); err != nil {
		return NotifyTarget{}, err
	}
	field := "notify " + name
	t := NotifyTarget{kind: kind, name: name}
	switch kind {
	case NotifyWebhook:
		if len(s.Command) > 0 {
			return NotifyTarget{}, invalid(field, "a webhook takes a url and a secret, no command")
		}
		if t.url, err = validateNotifyURL(field, s.URL); err != nil {
			return NotifyTarget{}, err
		}
		if s.Secret == "" {
			return NotifyTarget{}, invalid(field, "a webhook needs a secret to sign its deliveries")
		}
		if t.secret, err = validateCredential(field+" secret", s.Secret); err != nil {
			return NotifyTarget{}, err
		}
	case NotifyCommand:
		if s.URL != "" || s.Secret != "" {
			return NotifyTarget{}, invalid(field, "a command takes no url or secret")
		}
		if t.command, err = validateCommand(field, s.Command); err != nil {
			return NotifyTarget{}, err
		}
	}
	return t, nil
}

// validateNotifyURL accepts an absolute http or https URL with a host
// and no credentials in it.
func validateNotifyURL(field, raw string) (string, error) {
	if raw == "" {
		return "", invalid(field, "a webhook needs a url")
	}
	if len(raw) > maxNotifyURLLength {
		return "", invalid(field+" url", "must be at most 2048 characters")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", invalid(field+" url", "must be an absolute http or https URL")
	}
	if parsed.User != nil {
		return "", invalid(field+" url", "must not carry credentials: the secret signs the delivery")
	}
	return raw, nil
}

// validateCommand accepts a program given by its absolute path and up
// to 31 arguments without NUL bytes.
func validateCommand(field string, command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, invalid(field, "a command needs the program to run")
	}
	if len(command) > maxCommandArgs {
		return nil, invalid(field, "a command takes at most 32 words")
	}
	if !filepath.IsAbs(command[0]) {
		return nil, invalid(field, "the program must be an absolute path")
	}
	for _, word := range command {
		if len(word) > maxCommandArgBytes || strings.ContainsRune(word, 0) {
			return nil, invalid(field, "each word must be at most 1024 bytes and hold no NUL")
		}
	}
	return append([]string(nil), command...), nil
}

// Kind is the target's type.
func (t NotifyTarget) Kind() NotifyKind { return t.kind }

// Name is the target's name, unique within the bot.
func (t NotifyTarget) Name() string { return t.name }

// URL is where a webhook posts; empty for a command.
func (t NotifyTarget) URL() string { return t.url }

// Command is the program and its arguments; nil for a webhook.
func (t NotifyTarget) Command() []string { return append([]string(nil), t.command...) }

// Sign is the signature header value of a webhook delivery's body:
// `sha256=` and the HMAC-SHA256 of the body under the target's secret,
// the convention a webhook trigger checks. Empty for a command.
func (t NotifyTarget) Sign(body []byte) string {
	if t.kind != NotifyWebhook {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(t.secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Snapshot returns the target as plain data, the secret included: what
// the repository stores. Redact it before printing.
func (t NotifyTarget) Snapshot() NotifyTargetSnapshot {
	return NotifyTargetSnapshot{Type: t.kind.String(), Name: t.name, URL: t.url, Secret: t.secret, Command: t.Command()}
}
