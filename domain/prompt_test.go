package domain

import (
	"strings"
	"testing"
)

// The prompt carries the mission first and the task after it, each whole.
func TestPromptCarriesTheMissionAndTheObjective(t *testing.T) {
	mission, err := NewMission("Read the cluster with axlr-kubectl-ro.")
	if err != nil {
		t.Fatal(err)
	}
	objective, err := NewObjective("What Kubernetes version runs here?")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := NewPrompt(mission, objective)
	if err != nil {
		t.Fatal(err)
	}
	text := prompt.String()
	m, o := strings.Index(text, mission.String()), strings.Index(text, objective.String())
	if m < 0 || o < 0 || m > o || !strings.HasPrefix(text, "You are a bot.") {
		t.Fatalf("prompt %q", text)
	}
}

// A mission and an objective too long together are refused, never cut.
func TestPromptRefusesWhatDoesNotFit(t *testing.T) {
	mission, _ := NewMission(strings.Repeat("m", 2000))
	objective, _ := NewObjective(strings.Repeat("o", 7000))
	if _, err := NewPrompt(mission, objective); err == nil {
		t.Fatal("a 9000-character prompt was accepted")
	}
}

// The room an objective has beside a mission is exactly what makes a
// prompt: one byte more is refused, and the objective's own limit caps it.
func TestObjectiveRoomBesideAMissionIsExact(t *testing.T) {
	mission, err := NewMission(strings.Repeat("m", 2000))
	if err != nil {
		t.Fatal(err)
	}
	room := ObjectiveRoomBeside(mission)
	objective, err := NewObjective(strings.Repeat("o", room))
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := NewPrompt(mission, objective)
	if err != nil || len(prompt.String()) != MaxPromptLength {
		t.Errorf("at the room: %d bytes, %v", len(prompt.String()), err)
	}
	over, err := NewObjective(strings.Repeat("o", room+1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPrompt(mission, over); err == nil {
		t.Error("one byte over the room made a prompt")
	}
	if short, _ := NewMission("Hi."); ObjectiveRoomBeside(short) > MaxObjectiveLength {
		t.Errorf("room %d is over the objective's limit", ObjectiveRoomBeside(short))
	}
}
