package domain

import "strings"

const maxIdentifierLength = 64

// validateIdentifier accepts the identifiers the control plane uses in file
// names, URLs and events: 1 to 64 characters among lowercase letters,
// digits, dot, underscore and hyphen.
func validateIdentifier(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid(field, "must not be empty")
	}
	if len(value) > maxIdentifierLength {
		return "", invalid(field, "must be at most 64 characters")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return "", invalid(field, "may only use lowercase letters, digits, '.', '_' and '-'")
		}
	}
	return value, nil
}

// validateText accepts free text of bounded length without control
// characters other than newline and tab.
func validateText(field, raw string, max int) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid(field, "must not be empty")
	}
	if len(value) > max {
		return "", invalid(field, "is too long")
	}
	for _, r := range value {
		if r < 0x20 && r != '\n' && r != '\t' {
			return "", invalid(field, "must not contain control characters")
		}
	}
	return value, nil
}
