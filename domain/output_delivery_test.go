package domain

import (
	"errors"
	"testing"
	"time"
)

func sampleOutputDelivery(t *testing.T) OutputDelivery {
	t.Helper()
	target, err := NewOutputTarget(OutputTargetSnapshot{Name: "hook", Type: "webhook", URL: "https://x.test/", Secret: testOutputSecret})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewOutputDelivery("dlv-1", target, sampleIncident(t), "evt-1", EventIncidentOpened, []byte("{\n  \"a\": 1\n}"), t0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOutputDeliveryLifecycle(t *testing.T) {
	d := sampleOutputDelivery(t)
	if string(d.Payload()) != `{"a":1}` || d.Status() != DeliveryPending || !d.Due(t0) || d.Output() != "hook" || d.Kind() != OutputWebhook ||
		d.IncidentID() != "inc-1" || d.TenantID() != "ops" || d.EventID() != "evt-1" || d.Event() != EventIncidentOpened || !d.CreatedAt().Equal(t0) {
		t.Fatalf("new = %+v", d.Snapshot())
	}
	t1 := t0.Add(time.Second)
	if err := d.Retry("", t1, t1); err == nil {
		t.Error("a retry without a reason")
	}
	if err := d.Retry("HTTP 503", t0.Add(10*time.Second), t1); err != nil || d.Attempts() != 1 || d.Due(t1) || d.LastError() != "HTTP 503" {
		t.Fatalf("retry: %v", err)
	}
	if err := d.GiveUp("", t1); err == nil {
		t.Error("a give-up without a reason")
	}
	if err := d.Delivered(t1); err != nil || d.Status() != DeliveryDelivered || d.LastError() != "" || !d.UpdatedAt().Equal(t1) || !d.NextAttemptAt().Equal(t0.Add(10*time.Second)) {
		t.Fatalf("delivered: %v", err)
	}
	var serr StateError
	for name, step := range map[string]func() error{
		"deliver": func() error { return d.Delivered(t1) },
		"retry":   func() error { return d.Retry("x", t1, t1) },
		"give up": func() error { return d.GiveUp("x", t1) },
		"skip":    func() error { return d.Skip("x", t1) },
	} {
		if err := step(); !errors.As(err, &serr) {
			t.Errorf("%s after delivery: %v", name, err)
		}
	}
	failed := sampleOutputDelivery(t)
	if err := failed.GiveUp("HTTP 500", t1); err != nil || failed.Status() != DeliveryFailed {
		t.Errorf("give up: %v", err)
	}
	skipped := sampleOutputDelivery(t)
	if err := skipped.Skip("gone", t1); err != nil || skipped.Status() != DeliverySkipped || skipped.LastError() != "gone" {
		t.Errorf("skip: %v", err)
	}
	restored, err := RestoreOutputDelivery(d.Snapshot())
	if err != nil || restored.Snapshot().Status != "delivered" || restored.Attempts() != 2 {
		t.Errorf("restore: %v", err)
	}
}

func TestOutputDeliveryRefusals(t *testing.T) {
	target, _ := NewOutputTarget(OutputTargetSnapshot{Name: "hook", Type: "webhook", URL: "https://x.test/", Secret: testOutputSecret})
	incident := sampleIncident(t)
	if _, err := NewOutputDelivery("dlv-1", target, incident, "evt-1", EventIncidentOpened, []byte("{"), t0); err == nil {
		t.Error("a payload that is not JSON")
	}
	if _, err := NewOutputDelivery("dlv-1", target, incident, "evt-1", EventTaskCompleted, []byte("{}"), t0); err == nil {
		t.Error("an event about a task")
	}
	if _, err := NewOutputDelivery("dlv-1", target, incident, "", EventIncidentOpened, []byte("{}"), t0); err == nil {
		t.Error("no event id")
	}
	if _, err := NewOutputDelivery("dlv-1", target, incident, "evt-1", EventIncidentOpened, []byte("{}"), time.Time{}); err == nil {
		t.Error("no time")
	}
	good := sampleOutputDelivery(t).Snapshot()
	for name, mutate := range map[string]func(*OutputDeliverySnapshot){
		"id":       func(s *OutputDeliverySnapshot) { s.ID = "" },
		"output":   func(s *OutputDeliverySnapshot) { s.Output = "Bad" },
		"kind":     func(s *OutputDeliverySnapshot) { s.Kind = "email" },
		"incident": func(s *OutputDeliverySnapshot) { s.IncidentID = "" },
		"tenant":   func(s *OutputDeliverySnapshot) { s.TenantID = "" },
		"event id": func(s *OutputDeliverySnapshot) { s.EventID = "" },
		"event":    func(s *OutputDeliverySnapshot) { s.Event = "nope" },
		"status":   func(s *OutputDeliverySnapshot) { s.Status = "lost" },
		"attempts": func(s *OutputDeliverySnapshot) { s.Attempts = -1 },
		"updated":  func(s *OutputDeliverySnapshot) { s.UpdatedAt = time.Time{} },
		"payload":  func(s *OutputDeliverySnapshot) { s.Payload = []byte("{") },
	} {
		s := good
		mutate(&s)
		if _, err := RestoreOutputDelivery(s); err == nil {
			t.Errorf("%s: restored", name)
		}
	}
}
