package domain

import (
	"slices"
	"strings"
)

// Limits of the notes a prompt carries.
const (
	// MaxPromptNotes is how many notes a prompt carries: the newest.
	MaxPromptNotes = 8
	// MaxPromptNotesLength bounds the notes' block in a prompt.
	MaxPromptNotesLength = 1200
)

// notesHeading opens the notes' block in a prompt.
const notesHeading = "Recent events in your team, newest last (you heard them; nobody asked you to act on them):\n"

// EventNotes are the notes a task's prompt carries, oldest first.
type EventNotes struct {
	list []EventNote
}

// NewEventNotes keeps the newest MaxPromptNotes of the notes, oldest
// first.
func NewEventNotes(notes []EventNote) EventNotes {
	sorted := append([]EventNote(nil), notes...)
	slices.SortStableFunc(sorted, func(a, b EventNote) int { return a.At.Compare(b.At) })
	if len(sorted) > MaxPromptNotes {
		sorted = sorted[len(sorted)-MaxPromptNotes:]
	}
	return EventNotes{list: sorted}
}

// Empty reports whether there is no note.
func (n EventNotes) Empty() bool { return len(n.list) == 0 }

// List returns the notes, oldest first.
func (n EventNotes) List() []EventNote { return append([]EventNote(nil), n.list...) }

// Render writes the notes' block in at most limit characters (and at
// most MaxPromptNotesLength), dropping the oldest notes first; empty
// when not even one fits.
func (n EventNotes) Render(limit int) string {
	limit = min(limit, MaxPromptNotesLength)
	lines := make([]string, len(n.list))
	for i, note := range n.list {
		lines[i] = note.Line()
	}
	for len(lines) > 0 {
		block := notesHeading + strings.Join(lines, "\n")
		if len(block) <= limit {
			return block
		}
		lines = lines[1:]
	}
	return ""
}
