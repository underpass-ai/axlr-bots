package domain

import "strings"

// ToolName names a tool the way the tool gateway exposes it:
// dotted lowercase segments such as observability.query_logs.
type ToolName string

// NewToolName validates a tool name.
func NewToolName(raw string) (ToolName, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid("tool name", "must not be empty")
	}
	if len(value) > 128 {
		return "", invalid("tool name", "must be at most 128 characters")
	}
	for _, segment := range strings.Split(value, ".") {
		if segment == "" {
			return "", invalid("tool name", "must not have empty segments")
		}
		for _, r := range segment {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			default:
				return "", invalid("tool name", "may only use lowercase letters, digits, '_', '-' and '.' between segments")
			}
		}
	}
	return ToolName(value), nil
}

func (n ToolName) String() string { return string(n) }
