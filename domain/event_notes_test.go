package domain

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func notesOf(n int, message string) EventNotes {
	var list []EventNote
	for i := n; i >= 1; i-- {
		list = append(list, EventNote{From: "recepcion", Name: fmt.Sprintf("info.n%d", i), Message: fmt.Sprintf("%s %d", message, i), At: time.Date(2026, 10, 28, 10, i, 0, 0, time.UTC)})
	}
	return NewEventNotes(list)
}

func TestNotesKeepTheNewestEightOldestFirst(t *testing.T) {
	notes := notesOf(9, "note")
	list := notes.List()
	if len(list) != 8 || list[0].Name != "info.n2" || list[7].Name != "info.n9" || notes.Empty() {
		t.Fatalf("list = %+v", list)
	}
	if line := (EventNote{From: "a", Name: "info.x", Message: "two\nlines", At: time.Date(2026, 1, 1, 9, 5, 0, 0, time.UTC)}).Line(); line != "- 09:05 a (info.x): two lines" {
		t.Errorf("line = %q", line)
	}
	block := notes.Render(8000)
	if !strings.HasPrefix(block, notesHeading) || strings.Count(block, "\n- ") != 8 || !strings.HasSuffix(block, "note 9") {
		t.Errorf("block = %s", block)
	}
	long := notesOf(8, strings.Repeat("x", 200))
	if block := long.Render(8000); len(block) > MaxPromptNotesLength || strings.Contains(block, "info.n1)") || !strings.Contains(block, "info.n8") {
		t.Errorf("long block (%d) = %s", len(block), block)
	}
	if notes.Render(10) != "" || (EventNotes{}).Render(8000) != "" {
		t.Error("a block that does not fit is not empty")
	}
}

func TestNotesAreCutBeforeTheDirectory(t *testing.T) {
	directory := NewTeamDirectory([]TeamName{"ops"}, []Teammate{{ID: "worker-1", Purpose: "Deployer.", Where: "any node"}})
	notes := notesOf(8, strings.Repeat("n", 100))
	mission, _ := NewMission(strings.Repeat("m", 2000))
	objective, _ := NewObjective("Warn worker-1.")
	got, err := NewTeamPrompt(mission, objective, directory, notes)
	if err != nil || !strings.Contains(got.String(), "worker-1: Deployer.") || strings.Count(got.String(), "\n- 10:") != 8 {
		t.Fatalf("roomy = %v\n%s", err, got)
	}
	// Mission, objective, eight notes and the directory pass 8000: the
	// oldest notes go first, then the directory; mission and objective
	// stay whole.
	objective, _ = NewObjective(strings.Repeat("o", 5100))
	got, err = NewTeamPrompt(mission, objective, directory, notes)
	text := got.String()
	if err != nil || !strings.Contains(text, "worker-1: Deployer.") || strings.Contains(text, "info.n1)") || !strings.Contains(text, "info.n8") || !strings.HasSuffix(text, "o") || !strings.Contains(text, mission.String()) {
		t.Errorf("tight = %v (%d)\n%s", err, len(text), text)
	}
	objective, _ = NewObjective(strings.Repeat("o", 5880))
	got, err = NewTeamPrompt(mission, objective, directory, notes)
	if err != nil || strings.Contains(got.String(), "Recent events") || strings.Contains(got.String(), "teammates") {
		t.Errorf("full = %v", err)
	}
	if only, err := NewTeamPrompt(NewMissionOrPanic("Watch."), NewObjectiveOrPanic("Go."), TeamDirectory{}, notes); err != nil || !strings.Contains(only.String(), "Recent events") {
		t.Errorf("notes alone = %v %s", err, only)
	}
}

func TestATaskKeepsItsNotes(t *testing.T) {
	notes := notesOf(2, "note")
	task := Task{input: []byte(`{"name":"info.x"}`)}
	if task.HasNotes() || !task.Notes().Empty() {
		t.Error("a task without notes has some")
	}
	if err := task.KeepNotes(notes); err != nil || !task.HasNotes() || len(task.Notes().List()) != 2 || !strings.Contains(string(task.Input()), `"name":"info.x"`) {
		t.Fatalf("kept = %s %v", task.Input(), err)
	}
	if err := task.KeepNotes(notesOf(5, "other")); err != nil || len(task.Notes().List()) != 2 {
		t.Error("a task's notes were replaced")
	}
	manual := Task{}
	if err := manual.KeepNotes(EventNotes{}); err != nil || manual.Input() != nil {
		t.Error("no notes changed the input")
	}
	if err := manual.KeepNotes(notes); err != nil || !manual.HasNotes() {
		t.Errorf("manual = %s %v", manual.Input(), err)
	}
	array := Task{input: []byte(`[1,2]`)}
	if err := array.KeepNotes(notes); err != nil || !strings.Contains(string(array.Input()), `"input":[1,2]`) {
		t.Errorf("array = %s %v", array.Input(), err)
	}
	broken := Task{input: []byte(`"x"`)}
	if !broken.Notes().Empty() {
		t.Error("an input that is no object has notes")
	}
}

func NewMissionOrPanic(raw string) Mission {
	m, err := NewMission(raw)
	if err != nil {
		panic(err)
	}
	return m
}

func NewObjectiveOrPanic(raw string) Objective {
	o, err := NewObjective(raw)
	if err != nil {
		panic(err)
	}
	return o
}
