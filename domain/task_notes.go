package domain

import (
	"bytes"
	"encoding/json"
)

// KeepNotes records on the task the notes its prompt carried, as
// input.notes, so the task's detail shows what it knew and a later
// execution of it is given the same. A task that keeps notes already
// keeps those.
func (t *Task) KeepNotes(notes EventNotes) error {
	if notes.Empty() || t.HasNotes() {
		return nil
	}
	input := map[string]any{}
	if trimmed := bytes.TrimSpace(t.input); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &input); err != nil || input == nil {
			// An input that is no object is kept under input.
			input = map[string]any{"input": json.RawMessage(trimmed)}
		}
	}
	input["notes"] = notes.List()
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	parsed, err := NewTaskInput(raw)
	if err != nil {
		return err
	}
	t.input = parsed
	return nil
}

// HasNotes reports whether the task keeps the notes of its prompt.
func (t Task) HasNotes() bool {
	var input struct {
		Notes []EventNote `json:"notes"`
	}
	return json.Unmarshal(t.input, &input) == nil && input.Notes != nil
}

// Notes are the notes the task's prompt carried; empty when none.
func (t Task) Notes() EventNotes {
	var input struct {
		Notes []EventNote `json:"notes"`
	}
	if json.Unmarshal(t.input, &input) != nil {
		return EventNotes{}
	}
	return NewEventNotes(input.Notes)
}
