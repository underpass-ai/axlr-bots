package domain

import (
	"strings"
	"time"
)

// EventNote is one event a bot heard as a note: kept, and put in the
// prompt of its next task under "Recent events in your team".
type EventNote struct {
	From    string    `json:"from"`
	Name    string    `json:"name"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Line is the note as the prompt shows it: when, who, the name and the
// message, on one line.
func (n EventNote) Line() string {
	message := strings.Join(strings.Fields(n.Message), " ")
	return "- " + n.At.UTC().Format("15:04") + " " + n.From + " (" + n.Name + "): " + message
}
