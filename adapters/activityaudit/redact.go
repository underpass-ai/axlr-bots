package activityaudit

import "regexp"

// Redacted stands in for a secret in what the adapter keeps or publishes.
const Redacted = "<secret>"

// secretPatterns are the shapes of a credential that must never leave the
// machine in what the adapter publishes: GitHub's tokens, OpenAI-style keys, AWS access keys, Slack tokens, a
// private key's header, and a bearer token.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`),
}

// Redact replaces every secret the patterns find.
func Redact(text string) string {
	for _, pattern := range secretPatterns {
		text = pattern.ReplaceAllString(text, Redacted)
	}
	return text
}
