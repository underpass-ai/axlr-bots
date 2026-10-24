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
