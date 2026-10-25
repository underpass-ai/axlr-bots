package domain

import "strings"

// ValidateMediaType accepts a media type without parameters, type/subtype
// in lower case as AXLR writes it (text/markdown, application/json): what
// the control plane may send back as a Content-Type.
func ValidateMediaType(raw string) error {
	if raw == "" {
		return invalid("media type", "must not be empty")
	}
	if len(raw) > 127 {
		return invalid("media type", "must be at most 127 characters")
	}
	kind, sub, found := strings.Cut(raw, "/")
	if !found || kind == "" || sub == "" || strings.Contains(sub, "/") {
		return invalid("media type", "must be type/subtype")
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '/', r == '.', r == '+', r == '-', r == '_':
		default:
			return invalid("media type", "may only use lowercase letters, digits and . + - _ around one /")
		}
	}
	return nil
}
