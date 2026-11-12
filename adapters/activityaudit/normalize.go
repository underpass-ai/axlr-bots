package activityaudit

import (
	"regexp"
	"strings"
)

// MaxError bounds a normalised error, in characters.
const MaxError = 400

// The rewrites that take what varies out of an error, in order: what is
// left is the error's kind. Each placeholder is a word no rewrite after
// it matches.
var rewrites = []struct {
	pattern *regexp.Regexp
	with    string
}{
	// Quoted text is what a bot or a person wrote: a name a model made
	// up, a path, a question.
	{regexp.MustCompile(`"[^"\n]*"`), `"…"`},
	{regexp.MustCompile(`«[^»\n]*»`), `«…»`},
	{regexp.MustCompile("`[^`\n]*`"), "`…`"},
	{regexp.MustCompile(`(^|[\s(=:,\[])'[^'\n]*'`), `$1'…'`},
	{regexp.MustCompile(`\b[a-z][a-z0-9+.-]*://\S+`), "<url>"},
	{regexp.MustCompile(`(^|[^\w.])/(?:home|Users|tmp|run/user)/\S*`), "$1<path>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?`), "<time>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`), "<date>"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<id>"},
	// An id of the fleet's (task-…, exec-…, call_…, trace-…, read_…,
	// ws:…): a word, a separator and a run of hex or base-36 with a digit.
	{regexp.MustCompile(`\b[a-z]+[-_:][0-9a-z]*[0-9][0-9a-z]{5,}\b`), "<id>"},
	{regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`), "<addr>"},
	{regexp.MustCompile(`\blocalhost:\d+\b`), "<addr>"},
}

// hexRun is a candidate sha, whole or short: seven hex characters or
// more; it is one when it has a digit and a letter.
var hexRun = regexp.MustCompile(`(?i)\b[0-9a-f]{7,}\b`)

// number is what is left of a count, a size, a duration or a port; an
// HTTP status is kept: provider_failure (HTTP 504) and (HTTP 429) are
// different failures.
var number = regexp.MustCompile(`\bHTTP \d{3}\b|\d+(?:\.\d+)?`)

// Normalise is the first line of an error with what varies taken out:
// secrets redacted first, then quoted text, URLs, paths under /home,
// times, ids, shas, addresses and numbers replaced, an HTTP status kept,
// spaces folded, at most MaxError characters. Two occurrences of the
// same failure normalise to the same text.
func Normalise(text string) string {
	text = firstLine(Redact(text))
	for _, rewrite := range rewrites {
		text = rewrite.pattern.ReplaceAllString(text, rewrite.with)
	}
	text = hexRun.ReplaceAllStringFunc(text, func(run string) string {
		if strings.ContainsAny(run, "0123456789") && strings.ContainsAny(strings.ToLower(run), "abcdef") {
			return "<hex>"
		}
		return run
	})
	text = number.ReplaceAllStringFunc(text, func(n string) string {
		if strings.HasPrefix(n, "HTTP") {
			return n
		}
		return "N"
	})
	return cutRunes(strings.Join(strings.Fields(text), " "), MaxError)
}

// firstLine is the text up to its first line break, a literal \n of an
// escaped string included.
func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "\n"); i >= 0 {
		text = text[:i]
	}
	if i := strings.Index(text, `\n`); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// cutRunes keeps at most limit characters, the last an ellipsis when it
// was longer.
func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}
