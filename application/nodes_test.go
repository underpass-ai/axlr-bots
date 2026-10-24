package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

func registration(id string) NodeRegistration {
	return NodeRegistration{ID: id, HostName: id + ".example", Labels: map[string]string{"host": id}, Capabilities: domain.NodeCapabilities{AXLRVersion: "0.14.0", PolicySupported: true}}
}

// onlineNode registers a node and returns it Online.
func onlineNode(t *testing.T, h *harness, id string) domain.Node {
	t.Helper()
	node, err := NewRegisterNode(h.deps).Execute(context.Background(), registration(id))
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestRegisterHeartbeatAndListNodes(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node := onlineNode(t, h, "web-1")
	if node.Status() != domain.NodeOnline || node.HostName() != "web-1.example" || !node.Capabilities().PolicySupported {
		t.Errorf("node = %+v", node.Snapshot())
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "node.registered" || h.log.events[0].NodeID != "web-1" || h.log.events[0].TenantID != domain.PlatformTenant {
		t.Errorf("events = %v %+v", got, h.log.events)
	}
	if len(h.log.audits) != 1 || h.log.audits[0].Action != "node.register" {
		t.Errorf("audits = %+v", h.log.audits)
	}
	// Registering again describes the node anew.
	h.clock.now = t0.Add(time.Minute)
	again, err := NewRegisterNode(h.deps).Execute(ctx, NodeRegistration{ID: "web-1", HostName: "web-1.local", Capabilities: domain.NodeCapabilities{}})
	if err != nil || again.HostName() != "web-1.local" || again.Capabilities().PolicySupported || !again.RegisteredAt().Equal(t0) || !again.LastSeenAt().Equal(h.clock.now) {
		t.Errorf("re-register: %v %+v", err, again.Snapshot())
	}
	h.clock.now = t0.Add(2 * time.Minute)
	beat, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1")
	if err != nil || !beat.LastSeenAt().Equal(h.clock.now) || !beat.IsOnline() {
		t.Errorf("heartbeat: %v", err)
	}
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("unknown heartbeat: %v", err)
	}
	onlineNode(t, h, "db-1")
	nodes, err := NewListNodes(h.deps).Execute(ctx)
	if err != nil || len(nodes) != 2 || nodes[0].ID() != "db-1" {
		t.Errorf("list: %v %d", err, len(nodes))
	}
	if got, err := NewGetNode(h.deps).Execute(ctx, "web-1"); err != nil || got.ID() != "web-1" {
		t.Errorf("get: %v", err)
	}
	if _, err := NewGetNode(h.deps).Execute(ctx, "ghost"); !domain.IsNotFound(err) {
		t.Errorf("get missing: %v", err)
	}
	for _, bad := range []NodeRegistration{{ID: "Bad Id", HostName: "h"}, {ID: "ok", HostName: ""}, {ID: "ok", HostName: "h", Labels: map[string]string{"": "x"}}} {
		if _, err := NewRegisterNode(h.deps).Execute(ctx, bad); err == nil {
			t.Errorf("bad registration %+v accepted", bad)
		}
	}
	if _, err := NewRegisterNode(h.deps).Execute(ctx, NodeRegistration{ID: "web-1", HostName: ""}); err == nil {
		t.Error("bad re-registration accepted")
	}
}

func TestMarkOfflineNodes(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	h.clock.now = t0.Add(30 * time.Second)
	onlineNode(t, h, "web-2")
	h.log.events = nil
	h.clock.now = t0.Add(90 * time.Second)
	offline, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0)
	if err != nil || len(offline) != 1 || offline[0] != "web-1" {
		t.Fatalf("offline = %v %v", offline, err)
	}
	if node, _ := h.nodes.Get(ctx, "web-1"); node.Status() != domain.NodeOffline || !node.LastSeenAt().Equal(t0) {
		t.Errorf("web-1 = %+v", node.Snapshot())
	}
	if node, _ := h.nodes.Get(ctx, "web-2"); !node.IsOnline() {
		t.Error("web-2 went offline too early")
	}
	if got := h.log.eventTypes(); len(got) != 1 || got[0] != "node.offline" {
		t.Errorf("events = %v", got)
	}
	// Already offline nodes are left alone; a heartbeat brings one back.
	if offline, _ := NewMarkOfflineNodes(h.deps).Execute(ctx, time.Second); len(offline) != 1 || offline[0] != "web-2" {
		t.Errorf("second pass = %v", offline)
	}
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); err != nil {
		t.Fatal(err)
	}
	if node, _ := h.nodes.Get(ctx, "web-1"); !node.IsOnline() {
		t.Error("heartbeat did not bring the node back")
	}
}

func TestNodePortErrors(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	h.nodes.fail = errBoom
	if _, err := NewRegisterNode(h.deps).Execute(ctx, registration("web-1")); !errors.Is(err, errBoom) {
		t.Errorf("register save: %v", err)
	}
	h = newHarness()
	onlineNode(t, h, "web-1")
	h.nodes.fail = errBoom
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); !errors.Is(err, errBoom) {
		t.Errorf("heartbeat save: %v", err)
	}
	h.nodes.fail = nil
	h.log.fail = errBoom
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); !errors.Is(err, errBoom) {
		t.Errorf("heartbeat event: %v", err)
	}
	if _, err := NewRegisterNode(h.deps).Execute(ctx, registration("web-1")); !errors.Is(err, errBoom) {
		t.Errorf("register event: %v", err)
	}
	h.log.fail = nil
	h.nodes.listFail = errBoom
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("offline list: %v", err)
	}
	if _, err := NewListNodes(h.deps).Execute(ctx); !errors.Is(err, errBoom) {
		t.Errorf("list: %v", err)
	}
	h.nodes.listFail = nil
	h.clock.now = t0.Add(time.Hour)
	h.nodes.fail = errBoom
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("offline save: %v", err)
	}
	h.nodes.fail = nil
	h.log.fail = errBoom
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0); !errors.Is(err, errBoom) {
		t.Errorf("offline event: %v", err)
	}
}
