package axlr

import (
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestTheRunsWordsAfterAMessageAreOneReply(t *testing.T) {
	var heard []application.SaidReply
	tracker := newSaidTracker(func(reply application.SaidReply) { heard = append(heard, reply) })
	for _, line := range []string{
		`{"type":"text","text":"Working on the queue. "}`,
		`{"type":"steered","text":"check row F"}`,
		`{"type":"text","text":"OK, "}`,
		`{"type":"text","text":"row F next."}`,
		`{"type":"tool.requested","tool":"service_restart"}`,
		`{"type":"text","text":"Restarting."}`,
		`{"type":"steered","text":"nothing to say"}`,
		`{"type":"steered","text":"and row G?"}`,
		`{"type":"text","text":"Row G too."}`,
		`not json`,
	} {
		tracker.feed([]byte(line))
	}
	tracker.flush()
	if len(heard) != 2 || heard[0] != (application.SaidReply{InReplyTo: "check row F", Text: "OK, row F next."}) || heard[1] != (application.SaidReply{InReplyTo: "and row G?", Text: "Row G too."}) {
		t.Fatalf("%+v", heard)
	}
	long := newSaidTracker(func(reply application.SaidReply) { heard = append(heard, reply) })
	long.feed([]byte(`{"type":"steered","text":"x"}`))
	long.feed([]byte(`{"type":"text","text":"` + strings.Repeat("a", application.MaxSaidLength+5) + `"}`))
	long.feed([]byte(`{"type":"result"}`))
	if got := []rune(heard[2].Text); len(got) != application.MaxSaidLength || got[len(got)-1] != '…' {
		t.Fatalf("%d", len(got))
	}
	var none *saidTracker
	none.feed([]byte(`{"type":"steered"}`))
	none.flush()
	newSaidTracker(nil).feed([]byte(`{"type":"steered"}`))
}
