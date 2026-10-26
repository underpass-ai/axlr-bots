package domain

import "strings"

// Mission is the bot's declarative responsibility: what it is for and
// where its remit ends.
type Mission string

// NewMission validates a mission: 1 to 2000 characters of text.
func NewMission(raw string) (Mission, error) {
	value, err := validateText("mission", raw, 2000)
	if err != nil {
		return "", err
	}
	return Mission(value), nil
}

func (m Mission) String() string { return string(m) }

// MaxPurposeLength bounds the line a mission gives the team directory.
const MaxPurposeLength = 120

// Purpose is what the bot is for in one line, as its teammates read it in
// their directory: the mission's first sentence, cut at 120 characters.
func (m Mission) Purpose() string {
	text := strings.Join(strings.Fields(string(m)), " ")
	for i, r := range text {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(text) || text[i+1] == ' ') {
			text = text[:i+1]
			break
		}
	}
	if runes := []rune(text); len(runes) > MaxPurposeLength {
		text = strings.TrimSpace(string(runes[:MaxPurposeLength-1])) + "…"
	}
	return text
}
