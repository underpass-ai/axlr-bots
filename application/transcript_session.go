package application

import "github.com/underpass-ai/axlr-bots/domain"

// maxSessionID bounds a runtime session id.
const maxSessionID = 64

// validSession refuses a session id that is not 1 to 64 letters, digits,
// dashes or underscores: it names a row and, on the node, a file.
func validSession(session string) error {
	if session == "" || len(session) > maxSessionID {
		return domain.ValidationError{Field: "session", Reason: "is 1 to 64 characters"}
	}
	for _, r := range session {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return domain.ValidationError{Field: "session", Reason: "holds only letters, digits, - and _"}
		}
	}
	return nil
}
