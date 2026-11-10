package domain

import (
	"strings"
	"unicode/utf8"
)

// MaxQuestionLength is the most characters a question may have.
const MaxQuestionLength = 2000

// Question is what a bot asks another team through the team channel:
// complete in itself, since the team that answers sees nothing else of
// the asker's conversation.
type Question string

// NewQuestion validates a question: 1 to 2000 characters, trimmed,
// without control characters other than newlines and tabs.
func NewQuestion(raw string) (Question, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid("question", "must not be empty")
	}
	if !utf8.ValidString(value) {
		return "", invalid("question", "must be UTF-8")
	}
	if utf8.RuneCountInString(value) > MaxQuestionLength {
		return "", invalid("question", "must be at most 2000 characters")
	}
	for _, r := range value {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return "", invalid("question", "must not contain control characters")
		}
	}
	return Question(value), nil
}

func (q Question) String() string { return string(q) }
