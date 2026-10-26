package axlr

import (
	"encoding/json"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
)

// saidTracker gathers what a run says after each steered line, the
// operator's message reaching the model, until its next tool call or
// its end, and hands it over as one reply.
type saidTracker struct {
	hear    func(application.SaidReply)
	replyTo string
	words   strings.Builder
	active  bool
}

func newSaidTracker(hear func(application.SaidReply)) *saidTracker {
	return &saidTracker{hear: hear}
}

// feed reads one line of the run.
func (t *saidTracker) feed(line []byte) {
	if t == nil || t.hear == nil {
		return
	}
	var event eventLine
	if json.Unmarshal(line, &event) != nil {
		return
	}
	switch event.Type {
	case "steered":
		t.flush()
		t.active, t.replyTo = true, event.Text
	case "text":
		if t.active {
			t.words.WriteString(event.Text)
		}
	case "tool.requested", "decision.required", "completed", "failed", "result", "session.continued":
		t.flush()
	}
}

// flush hands over the words gathered, if any.
func (t *saidTracker) flush() {
	if t == nil || !t.active {
		return
	}
	words := strings.TrimSpace(t.words.String())
	t.active = false
	t.words.Reset()
	if words == "" {
		return
	}
	if runes := []rune(words); len(runes) > application.MaxSaidLength {
		words = string(runes[:application.MaxSaidLength-1]) + "…"
	}
	t.hear(application.SaidReply{InReplyTo: t.replyTo, Text: words})
}
