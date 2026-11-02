package domain

import (
	"testing"
	"time"
)

func TestNodeEnrollmentLabelsWinOverReportedOnes(t *testing.T) {
	reported, _ := NewNodeLabels(map[string]string{"zone": "us", "host": "x"})
	node, err := NewNode("web-1", "web-1", reported, NodeCapabilities{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	operator, _ := NewNodeLabels(map[string]string{"zone": "eu"})
	if err := node.Certify("ab12", operator, t0); err != nil {
		t.Fatal(err)
	}
	if labels := node.Labels(); labels["zone"] != "eu" || labels["host"] != "x" || len(labels) != 2 {
		t.Errorf("labels = %v", labels)
	}
	// The agent reports again: its zone still loses, its new keys count.
	again, _ := NewNodeLabels(map[string]string{"zone": "ap", "role": "db"})
	if err := node.Describe("web-1", again, NodeCapabilities{}, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if labels := node.Labels(); labels["zone"] != "eu" || labels["role"] != "db" || labels["host"] != "" {
		t.Errorf("labels after describe = %v", labels)
	}
	snapshot := node.Snapshot()
	if snapshot.Labels["zone"] != "eu" || snapshot.OperatorLabels["zone"] != "eu" || snapshot.ReportedLabels["zone"] != "ap" {
		t.Errorf("snapshot = %+v", snapshot)
	}
	// A key both name with the same value stays the operator's after a
	// round trip, so a later report cannot take it.
	same, _ := NewNodeLabels(map[string]string{"zone": "eu"})
	_ = node.Describe("web-1", same, NodeCapabilities{}, t0.Add(2*time.Second))
	restored, err := RestoreNode(node.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	_ = restored.Describe("web-1", again, NodeCapabilities{}, t0.Add(3*time.Second))
	if restored.Labels()["zone"] != "eu" || restored.OperatorLabels()["zone"] != "eu" || restored.ReportedLabels()["zone"] != "ap" {
		t.Errorf("restored labels = %v", restored.Labels())
	}
	var bare Node
	if bare.Labels() != nil {
		t.Errorf("a node with no labels has %v", bare.Labels())
	}
}

func TestNodeRecordWithOnlyEffectiveLabelsIsRead(t *testing.T) {
	legacy := NodeSnapshot{ID: "web-1", HostName: "web-1", Labels: map[string]string{"zone": "eu"}, Status: "Online", RegisteredAt: t0, LastSeenAt: t0}
	reported, err := RestoreNode(legacy)
	if err != nil || reported.ReportedLabels()["zone"] != "eu" || len(reported.OperatorLabels()) != 0 {
		t.Errorf("unenrolled: %v %+v", err, reported.Snapshot())
	}
	legacy.CertificateSerial = "ab12"
	enrolled, err := RestoreNode(legacy)
	if err != nil || enrolled.OperatorLabels()["zone"] != "eu" || len(enrolled.ReportedLabels()) != 0 {
		t.Errorf("enrolled: %v %+v", err, enrolled.Snapshot())
	}
	us, _ := NewNodeLabels(map[string]string{"zone": "us"})
	_ = enrolled.Describe("web-1", us, NodeCapabilities{}, t0.Add(time.Second))
	if enrolled.Labels()["zone"] != "eu" {
		t.Errorf("an enrolled node's old labels lost to a report: %v", enrolled.Labels())
	}
}
