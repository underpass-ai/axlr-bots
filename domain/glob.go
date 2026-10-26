package domain

import "strings"

// globMatches reports whether text fits pattern, in the runtime
// contract's words: * matches within a path segment (no '/'), ** across
// segments, ? one character other than '/'.
func globMatches(pattern, text string) bool {
	if pattern == "" {
		return text == ""
	}
	if strings.HasPrefix(pattern, "**") {
		rest := pattern[2:]
		for i := 0; i <= len(text); i++ {
			if globMatches(rest, text[i:]) {
				return true
			}
		}
		return false
	}
	switch pattern[0] {
	case '*':
		rest := pattern[1:]
		for i := 0; i <= len(text); i++ {
			if globMatches(rest, text[i:]) {
				return true
			}
			if i < len(text) && text[i] == '/' {
				return false
			}
		}
		return false
	case '?':
		return text != "" && text[0] != '/' && globMatches(pattern[1:], text[1:])
	}
	return text != "" && text[0] == pattern[0] && globMatches(pattern[1:], text[1:])
}
