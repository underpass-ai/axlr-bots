package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestASubscriptionExpectsFinalEvents(t *testing.T) {
	sub, err := NewSubscription(SubscriptionSnapshot{Topic: "qa.hallazgo.candidato", Mode: "task", Deliver: "team", Expects: []string{"qa.hallazgo.triado", " qa.hallazgo.flaky ", "qa.hallazgo.*"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := sub.Expects(); len(got) != 3 || got[1] != "qa.hallazgo.flaky" {
		t.Errorf("expects = %v", got)
	}
	if s := sub.Snapshot(); !reflect.DeepEqual(s.Expects, []string{"qa.hallazgo.triado", "qa.hallazgo.flaky", "qa.hallazgo.*"}) {
		t.Errorf("snapshot = %v", s.Expects)
	}
	crossed, err := NewSubscription(SubscriptionSnapshot{Topic: "incidencia.nueva", Team: "qa", Expects: []string{"incidencia.completada"}})
	if err != nil || len(crossed.Expects()) != 1 {
		t.Errorf("a crossing subscription without mode: %v %v", crossed.Expects(), err)
	}
	plain, _ := NewSubscription(SubscriptionSnapshot{Topic: "a", Mode: "task"})
	if plain.Expects() != nil || plain.Snapshot().Expects != nil {
		t.Error("a subscription without expects expects something")
	}
	bad := []SubscriptionSnapshot{
		{Topic: "a", Expects: []string{"b"}},
		{Topic: "a", Mode: "note", Expects: []string{"b"}},
		{Topic: "a", Team: "qa", Mode: "task", Expects: []string{"b"}, Status: "pending"},
		{Topic: "a", Mode: "task", Status: "pending", Expects: []string{"b"}},
		{Topic: "a", Mode: "task", Expects: []string{"Bad"}},
		{Topic: "a", Mode: "task", Expects: []string{"b", "b"}},
		{Topic: "a", Mode: "task", Expects: strings.Split("a,b,c,d,e,f,g,h,i", ",")},
	}
	for _, b := range bad {
		if _, err := NewSubscription(b); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func expectingTask(t *testing.T) Task {
	t.Helper()
	task := sampleTask(t)
	task.Expect([]EventPattern{"qa.hallazgo.triado", "qa.hallazgo.flaky"})
	return task
}

func TestATaskCarriesAndSettlesByDataID(t *testing.T) {
	task := expectingTask(t)
	if ok, err := task.Carry(CarriedEvent{EventID: "evt-1", Name: "qa.hallazgo.candidato", ID: "h-1", Message: strings.Repeat("é", 300)}); !ok || err != nil {
		t.Fatalf("carry: %v %v", ok, err)
	}
	if ok, _ := task.Carry(CarriedEvent{EventID: "evt-1", Name: "qa.hallazgo.candidato", ID: "h-1"}); ok {
		t.Error("the same event carried twice")
	}
	if _, err := task.Carry(CarriedEvent{EventID: "evt 2", Name: "x"}); err == nil {
		t.Error("a bad event carried")
	}
	if ok, _ := task.Carry(CarriedEvent{EventID: "evt-2", Name: "qa.hallazgo.candidato", ID: "h-2"}); !ok {
		t.Fatal("the second event was not carried")
	}
	if got := task.Carried(); len(got) != 2 || len([]rune(got[0].Message)) != 280 {
		t.Errorf("carried = %+v", got)
	}
	if task.RecordFinal("qa.hallazgo.triado", "h-1") {
		t.Error("a Pending task recorded a final")
	}
	if err := task.Start("exec-1", t0); err != nil {
		t.Fatal(err)
	}
	if task.CanCarry() {
		t.Error("a started task carries")
	}
	if ok, _ := task.Carry(CarriedEvent{EventID: "evt-3", Name: "qa.hallazgo.candidato", ID: "h-3"}); ok {
		t.Error("a started task carried")
	}
	switch {
	case task.RecordFinal("qa.hallazgo.otro", "h-1"):
		t.Error("a name outside expects counted")
	case task.RecordFinal("qa.hallazgo.triado", "h-9"):
		t.Error("a wrong data.id counted")
	case !task.RecordFinal("qa.hallazgo.flaky", "h-1"):
		t.Error("a flaky outcome did not count")
	case task.RecordFinal("qa.hallazgo.triado", "h-1"):
		t.Error("a settled event was settled twice")
	}
	if missing := task.Missing(); len(missing) != 1 || missing[0].ID != "h-2" {
		t.Errorf("missing = %+v", missing)
	}
	if !task.RecordFinal("qa.hallazgo.triado", "h-2") || len(task.Missing()) != 0 || len(task.Finals()) != 2 {
		t.Errorf("finals = %+v missing = %+v", task.Finals(), task.Missing())
	}
}

func TestACarriedEventWithoutAnIDIsSettledByAnyFinal(t *testing.T) {
	task := expectingTask(t)
	_, _ = task.Carry(CarriedEvent{EventID: "evt-1", Name: "qa.hallazgo.candidato"})
	_ = task.Start("exec-1", t0)
	if len(task.Missing()) != 1 {
		t.Fatal("an event without an id is settled before any final")
	}
	if !task.RecordFinal("qa.hallazgo.triado", "anything") || len(task.Missing()) != 0 {
		t.Errorf("missing = %+v", task.Missing())
	}
	if task.RecordFinal("qa.hallazgo.flaky", "other") {
		t.Error("a second final with nothing left to settle was recorded")
	}
}

func TestATaskCarriesAtMost32(t *testing.T) {
	task := expectingTask(t)
	for i := 0; i < MaxCarriedEvents; i++ {
		if ok, err := task.Carry(CarriedEvent{EventID: "evt-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Name: "qa.x"}); !ok || err != nil {
			t.Fatalf("carry %d: %v %v", i, ok, err)
		}
	}
	if task.CanCarry() {
		t.Error("a full task carries")
	}
	plain := sampleTask(t)
	if ok, _ := plain.Carry(CarriedEvent{EventID: "evt-1", Name: "qa.x"}); ok || plain.CanCarry() {
		t.Error("a task without expects carried")
	}
	if plain.Carried() != nil || plain.Finals() != nil || plain.Missing() != nil || plain.Expects() != nil || plain.RecordFinal("qa.x", "") {
		t.Error("a task without expects has a record")
	}
	plain.Expect(nil)
	if plain.Snapshot().Expects != nil {
		t.Error("Expect(nil) recorded")
	}
}

func TestDataIDReadsStringsAndNumbers(t *testing.T) {
	for data, want := range map[string]string{`{"id":"h-1"}`: "h-1", `{"id":42}`: "42", `{"id":4.5}`: "4.5", `{"id":true}`: "", `{}`: ""} {
		var decoded map[string]any
		_ = json.Unmarshal([]byte(data), &decoded)
		if got := DataID(decoded); got != want {
			t.Errorf("%s → %q", data, got)
		}
	}
}

func TestATasksExpectsRoundTrip(t *testing.T) {
	task := expectingTask(t)
	_, _ = task.Carry(CarriedEvent{EventID: "evt-1", Name: "qa.hallazgo.candidato", ID: "h-1", From: "qa-explorador", Redeliveries: 1, Tasks: []string{"task-0"}})
	_ = task.Start("exec-1", t0)
	task.RecordFinal("qa.hallazgo.triado", "h-1")
	s := task.Snapshot()
	raw, _ := json.Marshal(s)
	var back TaskSnapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreTask(back)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(restored.Snapshot())
	if string(again) != string(raw) {
		t.Errorf("round trip:\n%s\n%s", raw, again)
	}
	plain, _ := json.Marshal(sampleTask(t).Snapshot())
	if strings.Contains(string(plain), "expects") {
		t.Errorf("plain = %s", plain)
	}
	breaks := []func(*TaskExpectsSnapshot){
		func(e *TaskExpectsSnapshot) { e.Names = nil },
		func(e *TaskExpectsSnapshot) { e.Names = []string{"Bad"} },
		func(e *TaskExpectsSnapshot) { e.Carried[0].Name = "Bad" },
		func(e *TaskExpectsSnapshot) { e.Carried[0].EventID = "" },
		func(e *TaskExpectsSnapshot) { e.Carried[0].Redeliveries = 3 },
		func(e *TaskExpectsSnapshot) { e.Carried[0].Tasks = []string{"bad id"} },
		func(e *TaskExpectsSnapshot) { e.Finals[0].Name = "Bad" },
		func(e *TaskExpectsSnapshot) { e.Carried = make([]CarriedEvent, MaxCarriedEvents+1) },
	}
	for i, br := range breaks {
		var copied TaskSnapshot
		_ = json.Unmarshal(raw, &copied)
		br(copied.Expects)
		if _, err := RestoreTask(copied); err == nil {
			t.Errorf("break %d restored", i)
		}
	}
}
