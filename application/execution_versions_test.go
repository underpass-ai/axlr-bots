package application

import (
	"context"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAnAssignedExecutionRecordsWhatRunsIt(t *testing.T) {
	h := newHarness()
	h.deps.Build = fixedBuilds{running: domain.BuildIdentity{Version: "0.5.0", Revision: "cp-commit"}}
	ctx := context.Background()
	bot := enabledBot(t, h)
	reg := registration("web-1")
	reg.Capabilities.NodeVersion, reg.Capabilities.NodeRevision = "0.5.0", "node-commit"
	node, err := NewRegisterNode(h.deps).Execute(ctx, reg)
	if err != nil {
		t.Fatal(err)
	}
	submit(t, h, bot, "k-1")
	h.clock.now = t0.Add(time.Minute)
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	execution, _ := h.execs.Get(ctx, assignment.ExecutionID)
	v := execution.Versions()
	if v.Bot != bot.Version().String() || v.ControlPlane.Revision != "cp-commit" || v.Node == nil ||
		v.Node.Version != "0.5.0" || v.Node.Revision != "node-commit" || v.AXLR != "0.14.0" || !v.Proven() {
		t.Errorf("versions = %+v node = %+v", v, v.Node)
	}
}

func TestAnInProcessExecutionRecordsTheControlPlane(t *testing.T) {
	h := newHarness()
	h.deps.Build = fixedBuilds{running: domain.BuildIdentity{Version: "0.5.0", Revision: "cp-commit"}}
	v := versionsOf(h.deps, domain.Bot{}, nil)
	if v.Node != nil || v.AXLR != "" || v.ControlPlane.Revision != "cp-commit" || !v.Proven() {
		t.Errorf("versions = %+v", v)
	}
	h.deps.Build = nil
	if versionsOf(h.deps, domain.Bot{}, nil).Proven() {
		t.Error("no build reader proves nothing")
	}
}
