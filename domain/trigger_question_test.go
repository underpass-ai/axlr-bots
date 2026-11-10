package domain

import (
	"encoding/json"
	"testing"
)

// A question trigger is the responder's consent: it names the asking
// teams in from and nothing else.
func TestQuestionTriggerNamesTheTeamsItAnswers(t *testing.T) {
	trigger, err := NewTrigger(TriggerSnapshot{Type: "question", From: []string{"qa", "producto"}})
	if err != nil || trigger.Type() != TriggerQuestion || trigger.Name() != "question" {
		t.Fatalf("trigger = %+v %v", trigger, err)
	}
	if !trigger.AnswersFrom("qa") || !trigger.AnswersFrom("producto") || trigger.AnswersFrom("marketing") {
		t.Errorf("from = %v", trigger.From())
	}
	if s := trigger.Snapshot(); len(s.From) != 2 || s.From[0] != "qa" || s.Name != "question" {
		t.Errorf("snapshot = %+v", s)
	}
	out, _ := json.Marshal(trigger.Snapshot())
	if string(out) != `{"type":"question","name":"question","from":["qa","producto"]}` {
		t.Errorf("written: %s", out)
	}
	nine := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	for _, bad := range []TriggerSnapshot{
		{Type: "question"},
		{Type: "question", From: []string{}},
		{Type: "question", From: nine},
		{Type: "question", From: []string{"qa", "qa"}},
		{Type: "question", From: []string{"QA"}},
		{Type: "question", From: []string{"qa"}, Objective: "Answer {{.question}}"},
		{Type: "question", From: []string{"qa"}, Secret: "0123456789abcdef0123"},
		{Type: "question", From: []string{"qa"}, Every: "1h"},
		{Type: "question", From: []string{"qa"}, Events: []string{"build.*"}},
		{Type: "question", From: []string{"qa"}, Phase: "fix"},
		{Type: "question", From: []string{"qa"}, Topics: []string{"arn:aws:sns:eu-west-1:123456789012:alarms"}},
		{Type: "manual", From: []string{"qa"}},
		{Type: "delegation", From: []string{"qa"}},
	} {
		if _, err := NewTrigger(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

// question_answer names a task's trigger; a definition never declares it.
func TestQuestionAnswerIsATasksTriggerOnly(t *testing.T) {
	if kind, err := ParseTriggerType("question_answer"); err != nil || kind != TriggerQuestionAnswer {
		t.Fatalf("parse = %q %v", kind, err)
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "question_answer"}); err == nil {
		t.Error("a bot declaring question_answer")
	}
	ref := TriggerRef{Type: TriggerQuestionAnswer, Name: "qa"}
	if ref.Type.String() != "question_answer" {
		t.Errorf("ref = %+v", ref)
	}
}

func TestBotAnswersOnlyTheTeamsItsTriggersName(t *testing.T) {
	triggers, err := NewTriggers([]TriggerSnapshot{
		{Type: "manual"},
		{Type: "question", Name: "from-qa", From: []string{"qa"}},
		{Type: "question", Name: "from-producto", From: []string{"producto"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if found, ok := triggers.Question("producto"); !ok || found.Name() != "from-producto" {
		t.Errorf("found = %+v %v", found, ok)
	}
	bot := teamTestBot(t, "dev-go", "default", []string{"dev"})
	if bot.AnswersQuestionsFrom("qa") {
		t.Error("a bot that declares no question trigger answers")
	}
	bot.Wire(triggers, Placement{})
	if !bot.AnswersQuestionsFrom("qa") || !bot.AnswersQuestionsFrom("producto") || bot.AnswersQuestionsFrom("marketing") || bot.AnswersQuestionsFrom("") {
		t.Error("consent does not follow from")
	}
	if !bot.AcceptsTrigger(TriggerQuestion) || bot.AcceptsTrigger(TriggerQuestionAnswer) {
		t.Error("the question trigger is the bot's; question_answer never is")
	}
	// The other triggers answer nobody.
	manual, _ := triggers.Named("manual")
	if manual.AnswersFrom("qa") || manual.From() != nil {
		t.Errorf("manual from = %v", manual.From())
	}
}
