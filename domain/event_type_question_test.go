package domain

import "testing"

// The team channel's three events parse and are about a bot.
func TestQuestionEventsAreKnown(t *testing.T) {
	for _, kind := range []EventType{EventQuestionAsked, EventQuestionAnswered, EventQuestionRefused} {
		parsed, err := ParseEventType(kind.String())
		if err != nil || parsed != kind || kind.AboutGroup() || kind.AboutNode() || kind.AboutIncident() {
			t.Errorf("%s: %q %v", kind, parsed, err)
		}
	}
	if EventQuestionAsked != "bot.question.asked" || EventQuestionAnswered != "bot.question.answered" || EventQuestionRefused != "bot.question.refused" {
		t.Error("the ADR's names")
	}
}
