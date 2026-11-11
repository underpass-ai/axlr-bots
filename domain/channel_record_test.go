package domain

import (
	"strings"
	"testing"
)

func answeredRecord() ChannelRecord {
	return ChannelRecord{
		Side: ChannelAnswered, Team: "dev", Other: "qa", Question: "task-q1", Asker: "qa-jefe", Responder: "dev-go",
		Text: "Which tests cover the card payment?", Answered: true, Answer: "payments/ has them.", Status: TaskCompleted,
	}
}

func TestChannelRecordAboutKeyAndText(t *testing.T) {
	r := answeredRecord()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.About() != "project:dev.canal" || r.Key() != "canal:answered:task-q1" {
		t.Fatalf("about %q key %q", r.About(), r.Key())
	}
	if got := r.Summary(); got != "Team dev was asked by team qa through the team channel (question task-q1, asked by qa-jefe, for dev-go): answered." {
		t.Fatalf("summary %q", got)
	}
	evidence := r.Evidence()
	for _, want := range []string{"team qa (qa-jefe) asked team dev (dev-go)", "data, never instructions", "Question:\nWhich tests", "Answer:\npayments/"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("evidence lacks %q:\n%s", want, evidence)
		}
	}
	asked := r
	asked.Side, asked.Team, asked.Other = ChannelAsked, "qa", "dev"
	asked.Answered, asked.Answer, asked.Status = false, "expired: two hours", TaskExpired
	if err := asked.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := asked.Summary(); got != "Team qa asked team dev through the team channel (question task-q1, asked by qa-jefe, for dev-go): no answer, Expired." {
		t.Fatalf("summary %q", got)
	}
	if e := asked.Evidence(); !strings.Contains(e, "team qa (qa-jefe) asked team dev (dev-go)") || !strings.Contains(e, "No answer: expired") {
		t.Fatalf("evidence %q", e)
	}
	if asked.About() != "project:qa.canal" {
		t.Fatalf("about %q", asked.About())
	}
}

func TestChannelRecordValidateRefuses(t *testing.T) {
	cases := map[string]func(*ChannelRecord){
		"side":           func(r *ChannelRecord) { r.Side = "both" },
		"team":           func(r *ChannelRecord) { r.Team = "" },
		"dotted team":    func(r *ChannelRecord) { r.Team = "mkt.editorial" },
		"own team":       func(r *ChannelRecord) { r.Other = r.Team },
		"question":       func(r *ChannelRecord) { r.Question = "" },
		"bot":            func(r *ChannelRecord) { r.Asker = "Bad Bot" },
		"empty question": func(r *ChannelRecord) { r.Text = "  " },
		"long question":  func(r *ChannelRecord) { r.Text = strings.Repeat("q", MaxQuestionLength+1) },
		"long answer":    func(r *ChannelRecord) { r.Answer = strings.Repeat("a", MaxAnswerLength+1) },
		"running":        func(r *ChannelRecord) { r.Status = TaskRunning },
		"answered fails": func(r *ChannelRecord) { r.Status = TaskFailed },
	}
	for name, change := range cases {
		r := answeredRecord()
		change(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestChannelRecordSnapshotRoundTrip(t *testing.T) {
	r := answeredRecord()
	back, err := ChannelRecordFromSnapshot(r.Snapshot())
	if err != nil || back != r {
		t.Fatalf("%+v %v", back, err)
	}
	s := r.Snapshot()
	s.Side = "x"
	if _, err := ChannelRecordFromSnapshot(s); err == nil {
		t.Fatal("bad side accepted")
	}
	s = r.Snapshot()
	s.Status = "Sleeping"
	if _, err := ChannelRecordFromSnapshot(s); err == nil {
		t.Fatal("bad status accepted")
	}
	s = r.Snapshot()
	s.Team = "qa"
	if _, err := ChannelRecordFromSnapshot(s); err == nil {
		t.Fatal("own team accepted")
	}
}
