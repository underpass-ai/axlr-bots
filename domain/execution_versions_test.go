package domain

import (
	"strings"
	"testing"
)

func TestExecutionVersionsRoundTripAndProof(t *testing.T) {
	e := sampleExecution(t)
	if !e.Versions().IsZero() || e.Snapshot().Versions != nil {
		t.Fatal("an execution starts with no versions")
	}
	v := ExecutionVersions{Bot: "2.4.0", ControlPlane: BuildVersion{Version: "0.5.0", Revision: "9bb5442"},
		Node: &BuildVersion{Version: "0.5.0", Revision: "9bb5442"}, AXLR: "0.20.0"}
	if err := e.RecordVersions(v); err != nil {
		t.Fatal(err)
	}
	v.Node.Revision = "changed after"
	back, err := RestoreExecution(e.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	got := back.Versions()
	if got.Bot != "2.4.0" || got.Node == nil || got.Node.Revision != "9bb5442" || got.AXLR != "0.20.0" || !got.Proven() {
		t.Errorf("versions = %+v node = %+v", got, got.Node)
	}
	got.Node.Revision = ""
	if back.Versions().Node.Revision != "9bb5442" {
		t.Error("Versions hands out the execution's own record")
	}
}

func TestExecutionVersionsProvenByWhatRanIt(t *testing.T) {
	inProcess := ExecutionVersions{ControlPlane: BuildVersion{Version: "0.5.0", Revision: "abc"}}
	devNode := ExecutionVersions{ControlPlane: BuildVersion{Revision: "abc"}, Node: &BuildVersion{Version: "0.0.0-dev"}}
	if !inProcess.Proven() || devNode.Proven() || (ExecutionVersions{}).Proven() {
		t.Errorf("proven: in process %v, dev node %v, none %v", inProcess.Proven(), devNode.Proven(), (ExecutionVersions{}).Proven())
	}
}

func TestExecutionVersionsBounded(t *testing.T) {
	e := sampleExecution(t)
	long := strings.Repeat("x", 201)
	if err := e.RecordVersions(ExecutionVersions{Node: &BuildVersion{Revision: long}}); err == nil {
		t.Error("a 201-byte revision was kept")
	}
}
