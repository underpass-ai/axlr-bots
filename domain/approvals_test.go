package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const notifySecret = "0123456789abcdef-secret"

func sampleWait(since time.Time) ApprovalWait {
	return ApprovalWait{CallID: "call-1", Tool: "local_write", Since: since, Deadline: since.Add(time.Hour), OnExpire: ExpireDeny}
}

func TestApprovalsDefaultsAndBounds(t *testing.T) {
	a, err := NewApprovals(ApprovalsSnapshot{})
	if err != nil || a.TTL() != time.Hour || a.OnExpire() != ExpireDeny || len(a.Targets()) != 0 {
		t.Fatalf("defaults: %+v %v", a, err)
	}
	if s := a.Snapshot(); s.TTL != "1h" || s.OnExpire != "deny" || s.Notify != nil {
		t.Errorf("snapshot: %+v", s)
	}
	var zero Approvals
	if zero.TTL() != DefaultApprovalTTL || zero.OnExpire() != ExpireDeny {
		t.Error("a zero value must read as the defaults")
	}
	for raw, want := range map[string]string{"2m": "2m", "90s": "1m30s", "720h": "720h", "1h30m": "90m"} {
		a, err := NewApprovals(ApprovalsSnapshot{TTL: raw, OnExpire: "cancel"})
		if err != nil || a.Snapshot().TTL != want || a.OnExpire() != ExpireCancel {
			t.Errorf("ttl %s: %v %v", raw, a.Snapshot(), err)
		}
	}
	for _, bad := range []ApprovalsSnapshot{
		{TTL: "soon"}, {TTL: "30s"}, {TTL: "721h"}, {OnExpire: "approve"},
		{Notify: []NotifyTargetSnapshot{{Type: "email"}}},
		{Notify: []NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}, {Type: "command", Command: []string{"/bin/false"}}}},
		{Notify: make([]NotifyTargetSnapshot, 9)},
	} {
		if _, err := NewApprovals(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestNotifyTargets(t *testing.T) {
	webhook, err := NewNotifyTarget(NotifyTargetSnapshot{Type: "webhook", Name: "ops", URL: "https://hooks.example.com/axlr", Secret: notifySecret})
	if err != nil {
		t.Fatal(err)
	}
	if webhook.Kind() != NotifyWebhook || webhook.Name() != "ops" || webhook.URL() != "https://hooks.example.com/axlr" || webhook.Command() != nil {
		t.Errorf("webhook getters: %+v", webhook)
	}
	body := []byte(`{"taskId":"task-1"}`)
	signature := webhook.Sign(body)
	trigger, err := NewTrigger(TriggerSnapshot{Type: "webhook", SigningKey: notifySecret, Objective: "Handle {{.taskId}}"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(signature, "sha256=") || !trigger.Authenticates("", signature, body) {
		t.Errorf("signature %q is not the convention a webhook trigger checks", signature)
	}
	command, err := NewNotifyTarget(NotifyTargetSnapshot{Type: "command", Command: []string{"/usr/bin/notify-send", "AXLR"}})
	if err != nil {
		t.Fatal(err)
	}
	if command.Name() != "command" || command.Sign(body) != "" || len(command.Command()) != 2 {
		t.Errorf("command target: %+v", command)
	}
	snapshot := webhook.Snapshot()
	if snapshot.Secret != notifySecret || snapshot.Redacted().Secret != Redacted || command.Snapshot().Redacted().Secret != "" {
		t.Errorf("redaction: %+v", snapshot)
	}
	if _, err := NewNotifyTarget(snapshot.Redacted()); err == nil {
		t.Error("a redacted secret was taken as a secret")
	}
	for _, bad := range []NotifyTargetSnapshot{
		{Type: "webhook", URL: "https://x.example.com", Secret: "short"},
		{Type: "webhook", URL: "https://x.example.com"},
		{Type: "webhook", Secret: notifySecret},
		{Type: "webhook", URL: "ftp://x.example.com", Secret: notifySecret},
		{Type: "webhook", URL: "https://user:pw@x.example.com", Secret: notifySecret},
		{Type: "webhook", URL: "https://" + strings.Repeat("a", 2050), Secret: notifySecret},
		{Type: "webhook", URL: "https://x.example.com", Secret: notifySecret, Command: []string{"/bin/true"}},
		{Type: "command"},
		{Type: "command", Command: []string{"notify-send"}},
		{Type: "command", Command: []string{"/bin/true"}, URL: "https://x.example.com"},
		{Type: "command", Command: []string{"/bin/true", "a\x00b"}},
		{Type: "command", Command: make([]string, 33)},
		{Type: "command", Name: "Bad Name", Command: []string{"/bin/true"}},
	} {
		if _, err := NewNotifyTarget(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	a, err := NewApprovals(ApprovalsSnapshot{Notify: []NotifyTargetSnapshot{snapshot, command.Snapshot()}})
	if err != nil {
		t.Fatal(err)
	}
	if found, ok := a.Target("ops"); !ok || found.URL() != webhook.URL() {
		t.Error("target by name")
	}
	if _, ok := a.Target("nobody"); ok {
		t.Error("found a target that is not there")
	}
	redacted := a.Snapshot().Redacted()
	if redacted.Notify[0].Secret != Redacted || a.Snapshot().Notify[0].Secret != notifySecret {
		t.Error("redacting the snapshot changed the settings")
	}
	if (ApprovalsSnapshot{}).Redacted().Notify != nil {
		t.Error("redacting nothing made something")
	}
}

func TestBotKeepsItsApprovals(t *testing.T) {
	bot := sampleBot(t)
	if bot.Approvals().TTL() != time.Hour {
		t.Error("a new bot waits an hour")
	}
	approvals, err := NewApprovals(ApprovalsSnapshot{TTL: "2m", OnExpire: "cancel", Notify: []NotifyTargetSnapshot{{Type: "webhook", URL: "https://x.example.com/h", Secret: notifySecret}}})
	if err != nil {
		t.Fatal(err)
	}
	bot.SetApprovals(approvals)
	snapshot := bot.Snapshot()
	restored, err := RestoreBot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Approvals().TTL() != 2*time.Minute || restored.Approvals().OnExpire() != ExpireCancel || len(restored.Approvals().Targets()) != 1 {
		t.Errorf("restored approvals: %+v", restored.Approvals().Snapshot())
	}
	if snapshot.Redacted().Approvals.Notify[0].Secret != Redacted {
		t.Error("the bot's redacted snapshot shows the webhook secret")
	}
	if _, err := RestoreBot(snapshot.Redacted()); err == nil {
		t.Error("a redacted snapshot was restored")
	}
	snapshot.Approvals = ApprovalsSnapshot{}
	if legacy, err := RestoreBot(snapshot); err != nil || legacy.Approvals().TTL() != time.Hour {
		t.Errorf("a bot stored before approvals: %v", err)
	}
}

func TestTaskApprovalWait(t *testing.T) {
	task := sampleTask(t)
	if _, ok := task.Approval(); ok || task.ApprovalExpired(t0.Add(48*time.Hour)) {
		t.Fatal("a new task waits on nothing")
	}
	if err := task.Start("exec-1", t0); err != nil {
		t.Fatal(err)
	}
	bad := sampleWait(t0)
	bad.Deadline = t0
	if err := task.AwaitApproval(bad, t0); err == nil {
		t.Error("a deadline that is not after the wait began was accepted")
	}
	if err := task.AwaitApproval(sampleWait(t0), t0); err != nil {
		t.Fatal(err)
	}
	if err := task.RecordApproval(sampleWait(t0)); err == nil {
		t.Error("a second open wait was recorded")
	}
	wait, ok := task.Approval()
	if !ok || !wait.Open() || wait.CallID != "call-1" || task.ApprovalExpired(t0.Add(time.Hour)) || !task.ApprovalExpired(t0.Add(time.Hour+time.Nanosecond)) {
		t.Fatalf("wait: %+v", wait)
	}
	if err := task.ExpireApproval(t0.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := task.ExpireApproval(t0.Add(2 * time.Hour)); err == nil {
		t.Error("expired twice")
	}
	if task.ApprovalExpired(t0.Add(3*time.Hour)) || task.Status() != TaskWaitingApproval {
		t.Error("an expired wait is closed, the task still waits for its denial or cancel")
	}
	if err := task.Resume("exec-2", ApprovalExpired, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	wait, _ = task.Approval()
	if wait.Outcome != ApprovalExpired || wait.ClosedAt == nil || !wait.ClosedAt.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("the expiry was not kept: %+v", wait)
	}
	snapshot := task.Snapshot()
	restored, err := RestoreTask(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := restored.Approval(); got.Outcome != ApprovalExpired || got.Deadline != wait.Deadline {
		t.Errorf("restored wait: %+v", got)
	}
	snapshot.Approval.Outcome, snapshot.Approval.ClosedAt = "", nil
	if _, err := RestoreTask(snapshot); err == nil {
		t.Error("an open wait on a Running task was restored")
	}
	snapshot.Approval.Outcome = "lost"
	if _, err := RestoreTask(snapshot); err == nil {
		t.Error("an unknown outcome was restored")
	}
	if err := task.RecordApproval(sampleWait(t0)); err == nil {
		t.Error("a Running task recorded a wait")
	}
	if err := task.ExpireApproval(t0); err == nil {
		t.Error("a Running task expired an approval")
	}
}

func TestCancelClosesTheWait(t *testing.T) {
	task := sampleTask(t)
	_ = task.Start("exec-1", t0)
	_ = task.AwaitApproval(sampleWait(t0), t0)
	if err := task.Cancel(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if wait, _ := task.Approval(); wait.Outcome != ApprovalCancelled {
		t.Errorf("cancel: %+v", wait)
	}
	legacy := sampleTask(t)
	_ = legacy.Start("exec-1", t0)
	_ = legacy.AwaitApproval(sampleWait(t0), t0)
	snapshot := legacy.Snapshot()
	snapshot.Approval = nil
	old, err := RestoreTask(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.RecordApproval(ApprovalWait{Since: t0, Deadline: t0.Add(time.Minute), OnExpire: ExpireCancel, Outcome: ApprovalDenied}); err != nil {
		t.Fatal(err)
	}
	if wait, _ := old.Approval(); !wait.Open() || wait.OnExpire != ExpireCancel {
		t.Errorf("recorded wait: %+v", wait)
	}
	for _, bad := range []ApprovalWait{
		{Since: t0},
		{Since: t0, Deadline: t0.Add(time.Minute), OnExpire: "never"},
		{Since: t0, Deadline: t0.Add(time.Minute), OnExpire: ExpireDeny, ClosedAt: &t0},
		{Since: t0, Deadline: t0.Add(time.Minute), OnExpire: ExpireDeny, Outcome: ApprovalDenied},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestApprovalExpiredError(t *testing.T) {
	err := error(ApprovalExpiredError{TaskID: "task-1", CallID: "call-1", Deadline: t0})
	if !IsApprovalExpired(err) || !strings.Contains(err.Error(), "call-1") || !strings.Contains(err.Error(), "authorises nothing") {
		t.Errorf("error: %v", err)
	}
	if IsApprovalExpired(errors.New("other")) || strings.Contains(ApprovalExpiredError{TaskID: "task-1", Deadline: t0}.Error(), "call") {
		t.Error("misread")
	}
	for raw, want := range map[string]ApprovalExpiry{"": ExpireDeny, "deny": ExpireDeny, "cancel": ExpireCancel} {
		if got, err := ParseApprovalExpiry(raw); err != nil || got != want || got.String() != string(want) {
			t.Errorf("parse %q: %v %v", raw, got, err)
		}
	}
	if _, err := ParseEventType("bot.approval.expired"); err != nil {
		t.Error(err)
	}
}

func TestDeliveryLifecycle(t *testing.T) {
	target, _ := NewNotifyTarget(NotifyTargetSnapshot{Type: "command", Name: "desk", Command: []string{"/bin/true"}})
	if _, err := NewDelivery("dlv-1", "bot-1", "task-1", "exec-1", "call-1", target, json.RawMessage(`{`), t0); err == nil {
		t.Error("a payload that is not JSON was queued")
	}
	if _, err := NewDelivery("dlv-1", "bot-1", "task-1", "exec-1", "call-1", target, json.RawMessage(`{}`), time.Time{}); err == nil {
		t.Error("a zero time was accepted")
	}
	d, err := NewDelivery("dlv-1", "bot-1", "task-1", "exec-1", "call-1", target, json.RawMessage(`{"a":1}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Due(t0) || d.Due(t0.Add(-time.Second)) || d.Status() != DeliveryPending || d.Kind() != NotifyCommand || d.Target() != "desk" ||
		d.ID() != "dlv-1" || d.BotID() != "bot-1" || d.TaskID() != "task-1" || d.ExecutionID() != "exec-1" || d.CallID() != "call-1" ||
		string(d.Payload()) != `{"a":1}` || !d.CreatedAt().Equal(t0) || !d.UpdatedAt().Equal(t0) || !d.NextAttemptAt().Equal(t0) {
		t.Errorf("new delivery: %+v", d.Snapshot())
	}
	if err := d.Retry("", t0, t0); err == nil {
		t.Error("a retry without a reason")
	}
	d2, _ := NewDelivery("dlv-2", "bot-1", "task-1", "exec-1", "call-1", target, json.RawMessage(`{}`), t0)
	if err := d2.Retry("connection refused", t0.Add(10*time.Second), t0); err != nil || d2.Attempts() != 1 || d2.Due(t0) || !d2.Due(t0.Add(10*time.Second)) || d2.LastError() != "connection refused" {
		t.Fatalf("retry: %v %+v", err, d2.Snapshot())
	}
	if err := d2.Delivered(t0.Add(10 * time.Second)); err != nil || d2.Status() != DeliveryDelivered || d2.Attempts() != 2 || d2.LastError() != "" {
		t.Fatalf("delivered: %v", err)
	}
	for _, attempt := range []func() error{
		func() error { return d2.Delivered(t0) },
		func() error { return d2.Retry("x", t0, t0) },
		func() error { return d2.GiveUp("x", t0) },
		func() error { return d2.Skip("x", t0) },
	} {
		if attempt() == nil {
			t.Error("a delivered delivery changed")
		}
	}
	d3, _ := NewDelivery("dlv-3", "bot-1", "task-1", "exec-1", "", target, json.RawMessage(`{}`), t0)
	if err := d3.GiveUp("", t0); err == nil {
		t.Error("gave up without a reason")
	}
	d4, _ := NewDelivery("dlv-4", "bot-1", "task-1", "exec-1", "", target, json.RawMessage(`{}`), t0)
	if err := d4.GiveUp("HTTP 500", t0); err != nil || d4.Status() != DeliveryFailed || d4.Due(t0) {
		t.Errorf("give up: %v", err)
	}
	d5, _ := NewDelivery("dlv-5", "bot-1", "task-1", "exec-1", "", target, json.RawMessage(`{}`), t0)
	if err := d5.Skip("the task was decided", t0); err != nil || d5.Status() != DeliverySkipped {
		t.Errorf("skip: %v", err)
	}
	restored, err := RestoreDelivery(d2.Snapshot())
	if err != nil || restored.Snapshot().Status != "delivered" || restored.Attempts() != 2 {
		t.Fatalf("restore: %v", err)
	}
	for _, mutate := range []func(*DeliverySnapshot){
		func(s *DeliverySnapshot) { s.ID = "" },
		func(s *DeliverySnapshot) { s.BotID = "" },
		func(s *DeliverySnapshot) { s.TaskID = "" },
		func(s *DeliverySnapshot) { s.ExecutionID = "" },
		func(s *DeliverySnapshot) { s.Kind = "pager" },
		func(s *DeliverySnapshot) { s.Status = "lost" },
		func(s *DeliverySnapshot) { s.Target = "" },
		func(s *DeliverySnapshot) { s.Attempts = -1 },
		func(s *DeliverySnapshot) { s.UpdatedAt = time.Time{} },
		func(s *DeliverySnapshot) { s.Payload = json.RawMessage(`nope`) },
	} {
		s := d2.Snapshot()
		mutate(&s)
		if _, err := RestoreDelivery(s); err == nil {
			t.Errorf("restored %+v", s)
		}
	}
	for _, raw := range []string{"pending", "delivered", "failed", "skipped"} {
		if got, err := ParseDeliveryStatus(raw); err != nil || got.String() != raw {
			t.Error(raw)
		}
	}
	if _, err := ParseNotifyKind("pager"); err == nil {
		t.Error("an unknown kind")
	}
	if _, err := NewDeliveryID("Bad ID"); err == nil {
		t.Error("a bad id")
	}
}

func TestArtifactsAndReceipt(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	for _, good := range []string{"report.md", "a", "R2-d2_x.tar.gz"} {
		if err := ValidateArtifactName(good); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{"", ".hidden", "-x", "a/b", "a..b", "a b", strings.Repeat("a", 129), "é"} {
		if ValidateArtifactName(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if !(Artifact{Name: "r.md", Size: 3, SHA256: hash}).Verifiable() {
		t.Error("a delivered artifact must be verifiable")
	}
	for _, a := range []Artifact{
		{Name: "r.md", Size: 3},
		{Name: "r.md", Size: 3, SHA256: strings.Repeat("AB", 32)},
		{Name: "r.md", Size: MaxArtifactBytes + 1, SHA256: hash},
		{Name: "../r", Size: 3, SHA256: hash},
		{Name: "report.md", Size: 3, SHA256: hash, Source: ArtifactSourceAnswer},
	} {
		if a.Verifiable() {
			t.Errorf("verifiable: %+v", a)
		}
	}
	execution, _ := NewExecution("exec-1", "task-1", "bot-1", t0)
	if err := execution.RecordReceipt(json.RawMessage(`[1]`)); err == nil {
		t.Error("a receipt that is not an object")
	}
	if err := execution.RecordReceipt(json.RawMessage(`{"x":"` + strings.Repeat("a", MaxReceiptBytes) + `"}`)); err == nil {
		t.Error("an oversized receipt")
	}
	receipt := json.RawMessage(`{"schema":"axlr.execution-receipt/v1","complete":true}`)
	if err := execution.RecordReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	_ = execution.Complete(Result{Answer: "ok"}, Usage{}, "", t0)
	restored, err := RestoreExecution(execution.Snapshot())
	if err != nil || string(restored.Receipt()) != string(receipt) {
		t.Fatalf("receipt round trip: %v %s", err, restored.Receipt())
	}
	if err := restored.RecordReceipt(nil); err != nil || restored.Receipt() != nil {
		t.Error("clearing the receipt")
	}
	snapshot := execution.Snapshot()
	snapshot.Receipt = json.RawMessage(`"no"`)
	if _, err := RestoreExecution(snapshot); err == nil {
		t.Error("restored a bad receipt")
	}
}
