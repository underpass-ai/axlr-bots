package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestApproversAndTwoPersonsAreValidated(t *testing.T) {
	a, err := NewApprovals(ApprovalsSnapshot{Approvers: []string{"tirso", " oncall "}, TwoPersons: &TwoPersonsSnapshot{Namespaces: []string{"prod", "shop-prod"}, Severities: []string{"sev1", "sev2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Approvers(), []string{"tirso", "oncall"}) || !a.MayDecide("oncall") || !a.MayDecide("cli:tirso") || !a.MayDecide("Tirso") || a.MayDecide("cli:mallory") || a.MayDecide("") {
		t.Errorf("approvers = %v", a.Approvers())
	}
	for _, c := range []struct {
		namespace string
		severity  IncidentSeverity
		want      int
	}{{"prod", Sev3, 2}, {"shop-prod", Sev4, 2}, {"dev", Sev1, 2}, {"dev", Sev2, 2}, {"dev", Sev3, 1}, {"", "", 1}, {"", Sev3, 1}} {
		if got := a.ApprovalsNeeded([]string{c.namespace}, c.severity); got != c.want {
			t.Errorf("%q %q needs %d, want %d", c.namespace, c.severity, got, c.want)
		}
	}
	s := a.Snapshot()
	if !reflect.DeepEqual(s.Approvers, []string{"tirso", "oncall"}) || s.TwoPersons == nil || !reflect.DeepEqual(s.TwoPersons.Severities, []string{"sev1", "sev2"}) {
		t.Errorf("snapshot = %+v", s)
	}
	again, err := NewApprovals(s)
	if err != nil || !reflect.DeepEqual(again.Snapshot(), s) {
		t.Errorf("round trip = %+v %v", again.Snapshot(), err)
	}
	kept, err := a.WithAutoApprove([]ToolRuleSnapshot{{Tool: "rollback"}})
	if err != nil || !reflect.DeepEqual(kept.Approvers(), a.Approvers()) || !kept.TwoPersons().Set() {
		t.Errorf("autoApprove dropped the approvers: %+v %v", kept.Snapshot(), err)
	}
	none := DefaultApprovals()
	if !none.MayDecide("anyone") || none.MayDecide("anyone") != true || none.ApprovalsNeeded([]string{"prod"}, Sev1) != 1 || none.Snapshot().TwoPersons != nil || none.Snapshot().Approvers != nil {
		t.Errorf("defaults = %+v", none.Snapshot())
	}
	many := make([]string, 33)
	for i := range many {
		many[i] = "p" + string(rune('a'+i%26)) + strings.Repeat("x", i)
	}
	for name, s := range map[string]ApprovalsSnapshot{
		"empty approver":    {Approvers: []string{" "}},
		"approver space":    {Approvers: []string{"tirso garcia"}},
		"approver repeated": {Approvers: []string{"tirso", "tirso"}},
		"same person twice": {Approvers: []string{"tirso", "cli:Tirso"}},
		"policy approver":   {Approvers: []string{"policy:autoApprove"}},
		"two persons alone": {TwoPersons: &TwoPersonsSnapshot{Severities: []string{"sev1"}}},
		"one approver":      {Approvers: []string{"tirso"}, TwoPersons: &TwoPersonsSnapshot{Severities: []string{"sev1"}}},
		"approver too long": {Approvers: []string{strings.Repeat("a", 129)}},
		"too many":          {Approvers: many},
		"empty twoPersons":  {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{}},
		"bad namespace":     {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Namespaces: []string{"Prod"}}},
		"repeated ns":       {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Namespaces: []string{"prod", "prod"}}},
		"empty severity":    {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Severities: []string{""}}},
		"bad severity":      {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Severities: []string{"critical"}}},
		"repeated severity": {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Severities: []string{"sev1", "sev1"}}},
		"too many ns":       {Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Namespaces: make([]string, 33)}},
	} {
		if _, err := NewApprovals(s); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestValidateActor(t *testing.T) {
	if got, err := ValidateActor("decidedBy", " cli:tirso "); err != nil || got != "cli:tirso" {
		t.Errorf("actor = %q %v", got, err)
	}
	for _, bad := range []string{"", "a b", "a\tb", "a\x7fb", strings.Repeat("a", 129)} {
		if _, err := ValidateActor("decidedBy", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// awaiting is a task waiting on a call for needed people.
func awaiting(t *testing.T, needed int) Task {
	t.Helper()
	task := sampleTask(t)
	if err := task.Start("exec-1", t0); err != nil {
		t.Fatal(err)
	}
	if err := task.AwaitApproval(ApprovalWait{CallID: "c-1", Tool: "local_write", Since: t0, Deadline: t0.Add(time.Hour), OnExpire: ExpireDeny, ApprovalsNeeded: needed}, t0); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTwoApprovalsOnATask(t *testing.T) {
	task := awaiting(t, 2)
	if err := task.AddApproval("", t0); err == nil {
		t.Error("a nameless approval was recorded")
	}
	if err := task.FinalApproval("tirso", t0); err == nil {
		t.Error("the last approval came before the first")
	}
	if err := task.AddApproval("tirso", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	wait, _ := task.Approval()
	if !wait.Open() || len(wait.ApprovedBy) != 1 || !wait.ApprovedByActor("tirso") || wait.ApprovedByActor("oncall") || !wait.Deadline.Equal(t0.Add(time.Hour)) || task.Status() != TaskWaitingApproval {
		t.Fatalf("after the first: %+v", wait)
	}
	var state StateError
	if err := task.AddApproval("tirso", t0); !errors.As(err, &state) {
		t.Errorf("same actor again: %v", err)
	}
	if err := task.AddApproval("oncall", t0); err == nil {
		t.Error("the second approval was recorded without resuming")
	}
	if err := task.FinalApproval("tirso", t0); !errors.As(err, &state) {
		t.Errorf("same actor last: %v", err)
	}
	if _, err := RestoreTask(task.Snapshot()); err != nil {
		t.Errorf("restore after the first: %v", err)
	}
	if err := task.FinalApproval("oncall", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := task.Resume("exec-2", ApprovalApproved, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	closed, _ := task.Approval()
	if closed.Outcome != ApprovalApproved || len(closed.ApprovedBy) != 2 || closed.ApprovedBy[1].Actor != "oncall" {
		t.Errorf("closed = %+v", closed)
	}
	if _, err := RestoreTask(task.Snapshot()); err != nil {
		t.Errorf("restore after the second: %v", err)
	}
	if err := task.AddApproval("x", t0); err == nil {
		t.Error("an approval of a running task was recorded")
	}
	if err := task.FinalApproval("x", t0); err == nil {
		t.Error("a last approval of a running task was recorded")
	}
	if err := task.RaiseApprovalsNeeded(2); err == nil {
		t.Error("a running task's wait was raised")
	}

	one := awaiting(t, 0)
	if err := one.FinalApproval("tirso", t0); err != nil {
		t.Fatal(err)
	}
	if wait, _ := one.Approval(); wait.ApprovedBy != nil || wait.Needed() != 1 {
		t.Errorf("a one-person wait recorded %+v", wait)
	}
	if err := one.AddApproval("tirso", t0); err == nil {
		t.Error("a one-person wait took a first approval")
	}
	if err := one.RaiseApprovalsNeeded(1); err != nil {
		t.Error(err)
	}
	if err := one.RaiseApprovalsNeeded(3); err == nil {
		t.Error("raised to three")
	}
	if err := one.RaiseApprovalsNeeded(2); err != nil {
		t.Fatal(err)
	}
	if wait, _ := one.Approval(); wait.Needed() != 2 {
		t.Errorf("raised wait = %+v", wait)
	}
}

func TestTwoPersonWaitsAreValidated(t *testing.T) {
	at := t0.Add(time.Minute)
	base := func() ApprovalWait {
		return ApprovalWait{Since: t0, Deadline: t0.Add(time.Hour), OnExpire: ExpireDeny, ApprovalsNeeded: 2}
	}
	good := base()
	good.ApprovedBy = []ApprovalMark{{Actor: "tirso", At: at}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ApprovalWait){
		"three needed":       func(w *ApprovalWait) { w.ApprovalsNeeded = 3 },
		"negative":           func(w *ApprovalWait) { w.ApprovalsNeeded = -1 },
		"open and complete":  func(w *ApprovalWait) { w.ApprovedBy = []ApprovalMark{{"a", at}, {"b", at}} },
		"one person, marked": func(w *ApprovalWait) { w.ApprovalsNeeded = 0; w.ApprovedBy = []ApprovalMark{{"a", at}} },
		"nameless mark":      func(w *ApprovalWait) { w.ApprovedBy = []ApprovalMark{{"", at}} },
		"timeless mark":      func(w *ApprovalWait) { w.ApprovedBy = []ApprovalMark{{"a", time.Time{}}} },
		"same twice": func(w *ApprovalWait) {
			w.Outcome, w.ClosedAt = ApprovalApproved, &at
			w.ApprovedBy = []ApprovalMark{{"a", at}, {"a", at}}
		},
		"three marks": func(w *ApprovalWait) {
			w.Outcome, w.ClosedAt = ApprovalApproved, &at
			w.ApprovedBy = []ApprovalMark{{"a", at}, {"b", at}, {"c", at}}
		},
	} {
		w := base()
		change(&w)
		if err := w.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// What was stored before approvers and two persons reads back the same
// and writes back without the new fields.
func TestStoredJSONBeforeApproversRoundTrips(t *testing.T) {
	task := awaiting(t, 0)
	raw, _ := json.Marshal(task.Snapshot())
	if strings.Contains(string(raw), "approvalsNeeded") || strings.Contains(string(raw), "approvedBy") {
		t.Errorf("a one-person wait wrote %s", raw)
	}
	var snapshot TaskSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreTask(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := json.Marshal(restored.Snapshot()); string(again) != string(raw) {
		t.Errorf("task round trip:\n%s\n%s", raw, again)
	}
	if wait, _ := restored.Approval(); wait.Needed() != 1 {
		t.Errorf("an old wait needs %d", wait.Needed())
	}

	incident := sampleIncident(t)
	raw, _ = json.Marshal(incident.Snapshot())
	if strings.Contains(string(raw), "namespace") {
		t.Errorf("an incident without a namespace wrote %s", raw)
	}
	var stored IncidentSnapshot
	_ = json.Unmarshal(raw, &stored)
	back, err := RestoreIncident(stored)
	if err != nil || back.Namespace() != "" {
		t.Fatalf("incident: %v %q", err, back.Namespace())
	}

	var old ApprovalsSnapshot
	if err := json.Unmarshal([]byte(`{"ttl":"15m","onExpire":"deny","autoApprove":[{"tool":"rollback"}]}`), &old); err != nil {
		t.Fatal(err)
	}
	approvals, err := NewApprovals(old)
	if err != nil || approvals.Approvers() != nil || approvals.TwoPersons().Set() {
		t.Fatalf("old approvals: %+v %v", approvals.Snapshot(), err)
	}
	if out, _ := json.Marshal(approvals.Snapshot()); string(out) != `{"ttl":"15m","onExpire":"deny","autoApprove":[{"tool":"rollback"}]}` {
		t.Errorf("old approvals wrote %s", out)
	}
}

func TestActorKey(t *testing.T) {
	for _, same := range []string{"tirso", "cli:tirso", " Tirso ", "CLI:tirso"} {
		if ActorKey(same) != ActorKey("tirso") {
			t.Errorf("%q is not tirso", same)
		}
	}
	if ActorKey("cli:tirso") == ActorKey("oncall") {
		t.Error("two people are one")
	}
	wait := ApprovalWait{ApprovedBy: []ApprovalMark{{Actor: "tirso", At: t0}}}
	if !wait.ApprovedByActor("cli:tirso") || wait.ApprovedByActor("oncall") {
		t.Error("cli:tirso after tirso is another person")
	}
}

func TestIncidentNamespaces(t *testing.T) {
	incident := sampleIncident(t)
	if ignored := incident.RecordNamespaces(" "); ignored != nil || incident.Namespace() != "" || incident.Namespaces() != nil {
		t.Errorf("blank namespace: %v %q", ignored, incident.Namespace())
	}
	if ignored := incident.RecordNamespaces("a b", strings.Repeat("n", 254), "shop"); len(ignored) != 2 || incident.Namespace() != "shop" {
		t.Fatalf("ignored = %q, namespace = %q", ignored, incident.Namespace())
	}
	raw, _ := json.Marshal(incident.Snapshot())
	if !strings.Contains(string(raw), `"namespace":"shop"`) || strings.Contains(string(raw), "namespaces") {
		t.Errorf("one namespace wrote %s", raw)
	}
	incident.RecordNamespaces("shop", "prod")
	if !reflect.DeepEqual(incident.Namespaces(), []string{"shop", "prod"}) || incident.Namespace() != "shop" {
		t.Errorf("namespaces = %v", incident.Namespaces())
	}
	many := make([]string, 40)
	for n := range many {
		many[n] = "ns-" + strings.Repeat("x", n+1)
	}
	incident.RecordNamespaces(many...)
	if len(incident.Namespaces()) != 32 {
		t.Errorf("kept %d namespaces", len(incident.Namespaces()))
	}
	restored, err := RestoreIncident(incident.Snapshot())
	if err != nil || !reflect.DeepEqual(restored.Namespaces(), incident.Namespaces()) {
		t.Errorf("restored = %v %v", restored.Namespaces(), err)
	}
	old := sampleIncident(t).Snapshot()
	old.Namespace = "shop"
	if back, err := RestoreIncident(old); err != nil || !reflect.DeepEqual(back.Namespaces(), []string{"shop"}) {
		t.Errorf("a one-namespace incident: %v %v", back.Namespaces(), err)
	}
	broken := incident.Snapshot()
	broken.Namespaces[1] = "a\x01b"
	if _, err := RestoreIncident(broken); err == nil {
		t.Error("a broken namespace was restored")
	}
	disagree := incident.Snapshot()
	disagree.Namespace = "prod"
	if _, err := RestoreIncident(disagree); err == nil {
		t.Error("a namespace other than the first was restored")
	}
	firing, err := ParseAlertmanagerPayload(alertmanagerBody(t, "firing"))
	if err != nil || !reflect.DeepEqual(firing.Namespaces(), []string{"shop"}) {
		t.Errorf("payload namespaces = %v %v", firing.Namespaces(), err)
	}
	mixed := AlertmanagerPayload{Alerts: []AlertmanagerAlert{{Labels: map[string]string{"namespace": "dev"}}, {Labels: map[string]string{"namespace": "prod"}}, {Labels: map[string]string{"namespace": "dev"}}, {Labels: map[string]string{}}}}
	if got := mixed.Namespaces(); !reflect.DeepEqual(got, []string{"dev", "prod"}) {
		t.Errorf("mixed group = %v", got)
	}
	if got := (AlertmanagerPayload{CommonLabels: map[string]string{"namespace": "prod"}}).Namespaces(); !reflect.DeepEqual(got, []string{"prod"}) {
		t.Errorf("common = %v", got)
	}
	if (AlertmanagerPayload{}).Namespaces() != nil {
		t.Error("no alert named a namespace")
	}
	two, _ := NewApprovals(ApprovalsSnapshot{Approvers: []string{"a", "b"}, TwoPersons: &TwoPersonsSnapshot{Namespaces: []string{"prod"}}})
	if two.ApprovalsNeeded([]string{"dev", "prod"}, Sev4) != 2 || two.ApprovalsNeeded([]string{"dev"}, Sev4) != 1 || two.ApprovalsNeeded(nil, Sev4) != 1 {
		t.Error("any listed namespace is not enough")
	}
}
