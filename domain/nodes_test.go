package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func sampleNode(t *testing.T) Node {
	t.Helper()
	labels, err := NewNodeLabels(map[string]string{"host": "web-1", "zone": "eu"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := NewNode("web-1", "web-1.example", labels, NodeCapabilities{AXLRVersion: "0.14.0", PolicySupported: true}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestNodeLifecycle(t *testing.T) {
	node := sampleNode(t)
	if node.Status() != NodeRegistered || node.IsOnline() || node.HostName() != "web-1.example" || node.Labels()["zone"] != "eu" || !node.Capabilities().PolicySupported || !node.RegisteredAt().Equal(t0) || !node.LastSeenAt().Equal(t0) {
		t.Errorf("node = %+v", node.Snapshot())
	}
	t1 := t0.Add(time.Minute)
	node.Heartbeat(t1)
	if !node.IsOnline() || !node.LastSeenAt().Equal(t1) || !node.SeenWithin(t1.Add(30*time.Second), time.Minute) || node.SeenWithin(t1.Add(2*time.Minute), time.Minute) {
		t.Error("heartbeat wrong")
	}
	node.MarkOffline()
	if node.Status() != NodeOffline || !node.LastSeenAt().Equal(t1) {
		t.Error("offline wrong")
	}
	labels, _ := NewNodeLabels(map[string]string{"host": "web-1"})
	if err := node.Describe("web-1.local", labels, NodeCapabilities{}, t1.Add(time.Minute)); err != nil || !node.IsOnline() || node.HostName() != "web-1.local" || len(node.Labels()) != 1 || node.Capabilities().PolicySupported {
		t.Errorf("describe: %v %+v", err, node.Snapshot())
	}
	if err := node.Describe("", labels, NodeCapabilities{}, t1); err == nil {
		t.Error("empty host accepted")
	}
	if _, err := NewNode("web-1", "", nil, NodeCapabilities{}, t0); err == nil {
		t.Error("empty host accepted")
	}
	if _, err := NewNode("web-1", "h", nil, NodeCapabilities{}, time.Time{}); err == nil {
		t.Error("zero time accepted")
	}
	if _, err := NewNodeID("Web 1"); err == nil {
		t.Error("bad node id accepted")
	}
	if _, err := NewNodeLabels(map[string]string{"Bad Key": "v"}); err == nil {
		t.Error("bad label key accepted")
	}
	if _, err := NewNodeLabels(map[string]string{"k": "bad value"}); err == nil {
		t.Error("bad label value accepted")
	}
	if keys := node.Labels().Keys(); len(keys) != 1 || keys[0] != "host" {
		t.Errorf("keys = %v", keys)
	}
	if NodeLabels(nil).Copy() != nil {
		t.Error("nil labels copied to a map")
	}
	for raw, ok := range map[string]bool{"Registered": true, "Online": true, "Offline": true, "Gone": false} {
		if _, err := ParseNodeStatus(raw); (err == nil) != ok {
			t.Errorf("status %q: %v", raw, err)
		}
	}
}

func TestNodeSnapshotRoundTrip(t *testing.T) {
	node := sampleNode(t)
	node.Heartbeat(t0.Add(time.Second))
	raw, _ := json.Marshal(node.Snapshot())
	var snap NodeSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	back, err := RestoreNode(snap)
	if err != nil || back.Status() != NodeOnline || back.Labels()["host"] != "web-1" || !back.LastSeenAt().Equal(node.LastSeenAt()) || back.Capabilities().AXLRVersion != "0.14.0" {
		t.Errorf("restored = %v %+v", err, back.Snapshot())
	}
	bad := []func(*NodeSnapshot){
		func(s *NodeSnapshot) { s.ID = "" },
		func(s *NodeSnapshot) { s.HostName = "" },
		func(s *NodeSnapshot) { s.Labels = map[string]string{"Bad": "x"} },
		func(s *NodeSnapshot) { s.Status = "Gone" },
		func(s *NodeSnapshot) { s.RegisteredAt = time.Time{} },
		func(s *NodeSnapshot) { s.LastSeenAt = time.Time{} },
	}
	for i, mutate := range bad {
		s := node.Snapshot()
		mutate(&s)
		if _, err := RestoreNode(s); err == nil {
			t.Errorf("bad snapshot %d restored", i)
		}
	}
}

func TestPlacement(t *testing.T) {
	node := sampleNode(t)
	other, _ := NewNode("db-1", "db-1", NodeLabels{"zone": "us"}, NodeCapabilities{}, t0)
	any, _ := NewPlacement(nil, nil)
	if !any.IsEmpty() || !any.Admits(node) || !any.Admits(other) || any.Nodes() != nil || any.Selector() != nil {
		t.Error("empty placement wrong")
	}
	named, _ := NewPlacement([]string{"web-1"}, nil)
	if named.IsEmpty() || !named.Admits(node) || named.Admits(other) || len(named.Nodes()) != 1 {
		t.Error("named placement wrong")
	}
	selected, _ := NewPlacement(nil, map[string]string{"zone": "eu"})
	if !selected.Admits(node) || selected.Admits(other) || selected.Selector()["zone"] != "eu" {
		t.Error("selector wrong")
	}
	both, _ := NewPlacement([]string{"db-1"}, map[string]string{"zone": "eu"})
	if !both.Admits(node) || !both.Admits(other) {
		t.Error("named or selected should both admit")
	}
	// A dedicated node takes the bots that name or select it, not the rest.
	sidecar, _ := NewNode("svc-0", "svc-0", NodeLabels{"app": "svc"}, NodeCapabilities{Dedicated: true}, t0)
	bySelector, _ := NewPlacement(nil, map[string]string{"app": "svc"})
	byName, _ := NewPlacement([]string{"svc-0"}, nil)
	if any.Admits(sidecar) || !bySelector.Admits(sidecar) || !byName.Admits(sidecar) || selected.Admits(sidecar) {
		t.Error("dedicated node admitted wrong")
	}
	if _, err := NewPlacement([]string{"Bad Node"}, nil); err == nil {
		t.Error("bad node id accepted")
	}
	if _, err := NewPlacement(nil, map[string]string{"": "x"}); err == nil {
		t.Error("bad selector accepted")
	}
	snap := both.Snapshot()
	if len(snap.Nodes) != 1 || snap.Selector["zone"] != "eu" {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestTriggers(t *testing.T) {
	def := DefaultTriggers()
	if !def.Has(TriggerManual) || len(def.List()) != 1 || def.Snapshot()[0].Type != "manual" {
		t.Error("default triggers wrong")
	}
	if empty, err := NewTriggers(nil); err != nil || !empty.Has(TriggerManual) {
		t.Error("empty list should mean manual")
	}
	if _, err := NewTriggers([]TriggerSnapshot{{Type: "manual"}, {Type: "manual"}}); err == nil {
		t.Error("repeated trigger accepted")
	}
	if _, err := NewTriggers([]TriggerSnapshot{{Type: "unit-event"}}); err == nil {
		t.Error("unknown trigger accepted")
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "manual"}); err != nil {
		t.Error(err)
	}
	if TriggerManual.String() != "manual" {
		t.Error("String wrong")
	}
}

func TestBotWiring(t *testing.T) {
	bot := sampleBot(t)
	if !bot.AcceptsTrigger(TriggerManual) || !bot.Placement().IsEmpty() {
		t.Fatal("a new bot takes manual tasks anywhere")
	}
	placement, _ := NewPlacement([]string{"web-1"}, map[string]string{"zone": "eu"})
	bot.Wire(DefaultTriggers(), placement)
	_ = bot.Register(t0)
	raw, _ := json.Marshal(bot.Snapshot())
	var snap BotSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Triggers) != 1 || snap.Placement.Nodes[0] != "web-1" || snap.Placement.Selector["zone"] != "eu" {
		t.Errorf("snapshot = %+v", snap)
	}
	back, err := RestoreBot(snap)
	if err != nil || !back.Placement().Admits(sampleNode(t)) || !back.AcceptsTrigger(TriggerManual) {
		t.Errorf("restored = %v", err)
	}
	snap.Triggers = []TriggerSnapshot{{Type: "odd"}}
	if _, err := RestoreBot(snap); err == nil {
		t.Error("bad trigger restored")
	}
	snap.Triggers = nil
	snap.Placement.Nodes = []string{"Bad"}
	if _, err := RestoreBot(snap); err == nil {
		t.Error("bad placement restored")
	}
}

func TestExecutionAssignment(t *testing.T) {
	e := sampleExecution(t)
	if e.Kind() != ExecutionRun || e.IsAssigned() || !e.LeaseUntil().IsZero() || e.LeaseExpired(t0) {
		t.Fatal("a new execution is an unassigned run")
	}
	if err := e.Lease(t0.Add(time.Minute)); err == nil {
		t.Error("lease without a node accepted")
	}
	if err := e.AssignTo(""); err == nil {
		t.Error("empty node accepted")
	}
	if err := e.AssignTo("web-1"); err != nil || e.NodeID() != "web-1" || !e.LeaseUntil().IsZero() {
		t.Errorf("assign: %v", err)
	}
	if err := e.Lease(t0.Add(-time.Second)); err == nil {
		t.Error("lease before start accepted")
	}
	if err := e.Lease(t0.Add(time.Minute)); err != nil || e.LeaseExpired(t0.Add(30*time.Second)) || !e.LeaseExpired(t0.Add(2*time.Minute)) {
		t.Errorf("lease: %v", err)
	}
	if err := e.Decide(""); err == nil {
		t.Error("empty decision accepted")
	}
	if err := e.Decide("approve"); err != nil || e.Kind() != ExecutionDecide || e.Decision() != "approve" {
		t.Errorf("decide: %v", err)
	}
	raw, _ := json.Marshal(e.Snapshot())
	var snap ExecutionSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	back, err := RestoreExecution(snap)
	if err != nil || back.NodeID() != "web-1" || back.Kind() != ExecutionDecide || back.Decision() != "approve" || !back.LeaseUntil().Equal(e.LeaseUntil()) {
		t.Errorf("restored = %v %+v", err, back.Snapshot())
	}
	_ = e.Complete(Result{}, Usage{}, "", t0.Add(3*time.Minute))
	if e.LeaseExpired(t0.Add(time.Hour)) {
		t.Error("an ended execution has no lease to expire")
	}
	for _, attempt := range []func() error{func() error { return e.AssignTo("x") }, func() error { return e.Lease(t0.Add(time.Hour)) }, func() error { return e.Decide("deny") }} {
		if err := attempt(); err == nil {
			t.Error("an ended execution was changed")
		}
	}
	bad := []func(*ExecutionSnapshot){
		func(s *ExecutionSnapshot) { s.Kind = "odd" },
		func(s *ExecutionSnapshot) { s.Kind = "decide"; s.Decision = "" },
		func(s *ExecutionSnapshot) { s.NodeID = "Bad Node" },
		func(s *ExecutionSnapshot) { s.NodeID = ""; lease := t0; s.LeaseUntil = &lease },
	}
	for i, mutate := range bad {
		s := e.Snapshot()
		mutate(&s)
		if _, err := RestoreExecution(s); err == nil {
			t.Errorf("bad snapshot %d restored", i)
		}
	}
	for raw, ok := range map[string]bool{"": true, "run": true, "decide": true, "odd": false} {
		if _, err := ParseExecutionKind(raw); (err == nil) != ok {
			t.Errorf("kind %q: %v", raw, err)
		}
	}
}

func TestNodeEvents(t *testing.T) {
	event, err := NewNodeEvent("evt-1", EventNodeRegistered, "web-1", t0, map[string]any{"host": "web-1"})
	if err != nil || event.TenantID != PlatformTenant || event.NodeID != "web-1" || event.BotID != "" {
		t.Fatalf("node event: %v %+v", err, event)
	}
	if _, err := NewNodeEvent("evt-2", EventNodeRegistered, "", t0, nil); err == nil {
		t.Error("node event without a node accepted")
	}
	if _, err := NewNodeEvent("evt-3", EventTaskStarted, "web-1", t0, nil); err == nil {
		t.Error("task event as a node event accepted")
	}
	if _, err := NewEvent("evt-4", EventTaskStarted, "platform", "", "", "", t0, nil); err == nil {
		t.Error("task event without a bot accepted")
	}
	if !EventNodeHeartbeat.AboutNode() || EventExecutionAssigned.AboutNode() {
		t.Error("AboutNode wrong")
	}
	if _, err := ParseEventType("execution.reported"); err != nil {
		t.Error(err)
	}
}

func TestNodeCertificateAndRevocation(t *testing.T) {
	node := sampleNode(t)
	if node.CertificateSerial() != "" || node.IsRevoked() || node.AcceptsCertificate("") {
		t.Error("a fresh node holds a certificate")
	}
	if err := node.Certify("", nil, t0); err == nil {
		t.Error("an empty serial was recorded")
	}
	labels, _ := NewNodeLabels(map[string]string{"zone": "eu"})
	if err := node.Certify("ab12", labels, t0.Add(time.Second)); err != nil || node.CertificateSerial() != "ab12" || node.Labels()["zone"] != "eu" {
		t.Errorf("certify: %v %+v", err, node.Snapshot())
	}
	if !node.AcceptsCertificate("ab12") || node.AcceptsCertificate("ab13") {
		t.Error("the serial check is wrong")
	}
	// Rotation: a new certificate supersedes the old one.
	if err := node.Certify("cd34", nil, t0.Add(2*time.Second)); err != nil || node.AcceptsCertificate("ab12") || !node.AcceptsCertificate("cd34") || node.Labels()["zone"] != "eu" {
		t.Errorf("rotate: %v", err)
	}
	if err := node.Revoke(time.Time{}); err == nil {
		t.Error("a zero revocation time was accepted")
	}
	if err := node.Revoke(t0.Add(time.Minute)); err != nil || !node.IsRevoked() || node.Status() != NodeRevoked || node.AcceptsCertificate("cd34") || node.IsOnline() {
		t.Errorf("revoke: %v %+v", err, node.Snapshot())
	}
	if err := node.Revoke(t0.Add(time.Minute)); err == nil {
		t.Error("revoked twice")
	}
	if err := node.Heartbeat(t0.Add(2 * time.Minute)); err == nil {
		t.Error("a revoked node heartbeat")
	}
	if err := node.Describe("host", nil, NodeCapabilities{}, t0.Add(2*time.Minute)); err == nil {
		t.Error("a revoked node registered")
	}
	snapshot := node.Snapshot()
	if snapshot.Status != "Revoked" || snapshot.RevokedAt == nil || snapshot.CertificateSerial != "cd34" {
		t.Errorf("snapshot = %+v", snapshot)
	}
	restored, err := RestoreNode(snapshot)
	if err != nil || !restored.IsRevoked() || !restored.RevokedAt().Equal(t0.Add(time.Minute)) || restored.CertificateSerial() != "cd34" {
		t.Errorf("restore: %v %+v", err, restored.Snapshot())
	}
	snapshot.RevokedAt = nil
	if _, err := RestoreNode(snapshot); err == nil {
		t.Error("a revoked node without a time was restored")
	}
	// Enrolling again readmits the node.
	if err := node.Certify("ef56", nil, t0.Add(3*time.Minute)); err != nil || node.IsRevoked() || node.Status() != NodeRegistered || !node.RevokedAt().IsZero() || !node.AcceptsCertificate("ef56") {
		t.Errorf("readmit: %v %+v", err, node.Snapshot())
	}
	if err := node.Heartbeat(t0.Add(4 * time.Minute)); err != nil || !node.IsOnline() {
		t.Errorf("heartbeat after readmission: %v", err)
	}
	if err := node.Describe("host", nil, NodeCapabilities{}, t0.Add(5*time.Minute)); err != nil || node.Labels()["zone"] != "eu" {
		t.Errorf("a registration without labels dropped them: %v %v", err, node.Labels())
	}
	if _, err := ParseNodeStatus("Revoked"); err != nil {
		t.Error(err)
	}
}

func TestEnrollmentTokens(t *testing.T) {
	labels, _ := NewNodeLabels(map[string]string{"zone": "eu"})
	secret := "a-secret-of-at-least-sixteen-characters"
	bad := []struct {
		id     EnrollmentID
		node   NodeID
		secret string
		now    time.Time
		ttl    time.Duration
	}{
		{"", "web-2", secret, t0, time.Hour},
		{"enr-1", "", secret, t0, time.Hour},
		{"enr-1", "web-2", "short", t0, time.Hour},
		{"enr-1", "web-2", secret, time.Time{}, time.Hour},
		{"enr-1", "web-2", secret, t0, 0},
	}
	for _, tc := range bad {
		if _, err := NewEnrollment(tc.id, tc.node, labels, tc.secret, tc.now, tc.ttl); err == nil {
			t.Errorf("%+v accepted", tc)
		}
	}
	e, err := NewEnrollment("enr-1", "web-2", labels, secret, t0, time.Hour)
	if err != nil || e.ID() != "enr-1" || e.NodeID() != "web-2" || e.Labels()["zone"] != "eu" || !e.CreatedAt().Equal(t0) || !e.ExpiresAt().Equal(t0.Add(time.Hour)) || e.IsUsed() {
		t.Fatalf("new: %v %+v", err, e.Snapshot())
	}
	if e.Snapshot().SecretHash == secret || len(e.Snapshot().SecretHash) != 64 || e.Snapshot().UsedAt != nil {
		t.Error("the secret is stored, or the hash is not SHA-256")
	}
	if err := e.Consume("wrong-secret-of-sixteen-chars", t0.Add(time.Minute)); !IsForbidden(err) || e.IsUsed() {
		t.Errorf("wrong secret: %v", err)
	}
	if err := e.Consume(secret, t0.Add(time.Minute)); err != nil || !e.IsUsed() || !e.UsedAt().Equal(t0.Add(time.Minute)) {
		t.Errorf("consume: %v", err)
	}
	var state StateError
	if err := e.Consume(secret, t0.Add(2*time.Minute)); !errors.As(err, &state) || !strings.Contains(err.Error(), "used at") {
		t.Errorf("reuse: %v", err)
	}
	expired, _ := NewEnrollment("enr-2", "web-3", nil, secret, t0, time.Minute)
	if err := expired.Consume(secret, t0.Add(2*time.Minute)); !errors.As(err, &state) || !strings.Contains(err.Error(), "expired at") || !expired.IsExpired(t0.Add(2*time.Minute)) {
		t.Errorf("expired: %v", err)
	}
	snapshot := e.Snapshot()
	restored, err := RestoreEnrollment(snapshot)
	if err != nil || !restored.IsUsed() || restored.Labels()["zone"] != "eu" || restored.Snapshot().SecretHash != snapshot.SecretHash {
		t.Errorf("restore: %v %+v", err, restored.Snapshot())
	}
	for name, mutate := range map[string]func(*EnrollmentSnapshot){
		"id":      func(s *EnrollmentSnapshot) { s.ID = "Bad Id" },
		"node":    func(s *EnrollmentSnapshot) { s.NodeID = "" },
		"labels":  func(s *EnrollmentSnapshot) { s.Labels = map[string]string{"Bad Key": "v"} },
		"hash":    func(s *EnrollmentSnapshot) { s.SecretHash = "short" },
		"created": func(s *EnrollmentSnapshot) { s.CreatedAt = time.Time{} },
		"expires": func(s *EnrollmentSnapshot) { s.ExpiresAt = s.CreatedAt },
	} {
		broken := e.Snapshot()
		mutate(&broken)
		if _, err := RestoreEnrollment(broken); err == nil {
			t.Errorf("%s: bad snapshot restored", name)
		}
	}
	if (ForbiddenError{Reason: "x"}).Error() != "forbidden: x" || IsForbidden(errors.New("other")) {
		t.Error("forbidden error wording")
	}
	for _, kind := range []EventType{EventNodeEnrolled, EventNodeRevoked} {
		if _, err := NewNodeEvent("evt-1", kind, "web-2", t0, nil); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}
