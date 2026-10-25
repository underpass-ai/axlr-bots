package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func delivered(name, content string) domain.Artifact {
	return domain.Artifact{Name: name, MediaType: "text/markdown", Size: int64(len(content)), SHA256: digest(content), Reference: ".axlr/deliverables/sess-1/" + name, Source: "inline", SessionID: "sess-1", CallID: "deliver-1"}
}

// runningOnNode starts a task on a node and returns its execution id.
func runningOnNode(t *testing.T, h *harness) (domain.Node, domain.ExecutionID) {
	t.Helper()
	bot := enabledBot(t, h)
	node := onlineNode(t, h, "web-1")
	submit(t, h, bot, "k")
	assignment, found, err := NewNextAssignment(h.deps).Execute(context.Background(), node.ID())
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", err, found)
	}
	h.log.events, h.log.audits = nil, nil
	return node, assignment.ExecutionID
}

func TestANodeUploadsThenReportsItsArtifacts(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	content := "# What failed\n\nThe disk.\n"
	artifact := delivered("report.md", content)
	stored, err := NewUploadArtifact(h.deps).Execute(ctx, node.ID(), execution, artifact, strings.NewReader(content))
	if err != nil || !stored.Stored {
		t.Fatalf("upload: %v %+v", err, stored)
	}
	if audit := h.log.audits[0]; audit.Action != "artifact.upload" || audit.Outcome != domain.AuditOK || audit.Details["sha256"] != artifact.SHA256 {
		t.Errorf("audit = %+v", audit)
	}
	notUploaded := delivered("diff.patch", "--- a\n+++ b\n")
	claimed := notUploaded
	claimed.Stored = true
	report := ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", Artifacts: []domain.Artifact{artifact, claimed}},
		Receipt: json.RawMessage(`{"schema":"axlr.execution-receipt/v1","complete":true}`)}
	result, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), execution, report)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := result.Execution.Result().Artifacts
	if len(artifacts) != 3 || !artifacts[0].Stored || artifacts[1].Stored || artifacts[2].Source != domain.ArtifactSourceAnswer || artifacts[2].Name != ReportArtifactName {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	if ReportReference(result.Execution) != artifacts[2].Reference {
		t.Errorf("the report is the answer, not the delivered report.md: %s", ReportReference(result.Execution))
	}
	if string(result.Execution.Receipt()) != string(report.Receipt) {
		t.Errorf("receipt = %s", result.Execution.Receipt())
	}
	completed := h.log.events[0]
	if names, _ := completed.Payload["artifacts"].([]string); len(names) != 1 || names[0] != "report.md" {
		t.Errorf("completed event = %+v", completed.Payload)
	}
	got, body, err := NewGetArtifact(h.deps).Execute(ctx, execution, "report.md")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	_ = body.Close()
	if string(data) != content || got.MediaType != "text/markdown" || got.SHA256 != artifact.SHA256 {
		t.Errorf("served %q %+v", data, got)
	}
	if _, _, err := NewGetArtifact(h.deps).Execute(ctx, execution, "diff.patch"); !domain.IsNotFound(err) {
		t.Errorf("an artifact that was not kept: %v", err)
	}
	if _, _, err := NewGetArtifact(h.deps).Execute(ctx, execution, "../x"); err == nil {
		t.Error("a path was taken as a name")
	}
	if _, _, err := NewGetArtifact(h.deps).Execute(ctx, "exec-missing", "report.md"); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	h.store.openFail = errBoom
	if _, _, err := NewGetArtifact(h.deps).Execute(ctx, execution, "report.md"); !errors.Is(err, errBoom) {
		t.Errorf("open: %v", err)
	}
	// The execution ended: nothing more is taken for it.
	if _, err := NewUploadArtifact(h.deps).Execute(ctx, node.ID(), execution, notUploaded, strings.NewReader("--- a\n+++ b\n")); err == nil {
		t.Error("an upload for an ended execution was taken")
	}
}

func TestAnUploadIsCheckedBeforeItIsKept(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	uc := NewUploadArtifact(h.deps)
	content := "hello"
	good := delivered("hello.txt", content)
	cases := []struct {
		name     string
		node     domain.NodeID
		artifact domain.Artifact
		body     string
	}{
		{"another node", "db-1", good, content},
		{"bytes that do not hash", node.ID(), good, "HELLO"},
		{"more bytes than listed", node.ID(), good, content + "!"},
		{"fewer bytes than listed", node.ID(), good, "hell"},
		{"no hash", node.ID(), domain.Artifact{Name: "a.txt", Size: 5}, content},
		{"too large", node.ID(), domain.Artifact{Name: "a.txt", Size: domain.MaxArtifactBytes + 1, SHA256: digest(content)}, content},
		{"a path for a name", node.ID(), domain.Artifact{Name: "../a.txt", Size: 5, SHA256: digest(content)}, content},
		{"a bad media type", node.ID(), domain.Artifact{Name: "a.txt", MediaType: "text/html; charset=x", Size: 5, SHA256: digest(content)}, content},
	}
	for _, c := range cases {
		if _, err := uc.Execute(ctx, c.node, execution, c.artifact, strings.NewReader(c.body)); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if stored, _ := h.store.StoredArtifacts(ctx, execution); len(stored) != 0 {
		t.Fatalf("a refused upload was kept: %+v", stored)
	}
	denied := 0
	for _, a := range h.log.audits {
		if a.Action == "artifact.upload" && a.Outcome == domain.AuditDenied {
			denied++
		}
	}
	if denied != len(cases) {
		t.Errorf("denied audits = %d, want %d", denied, len(cases))
	}
	if _, err := uc.Execute(ctx, node.ID(), "exec-missing", good, strings.NewReader(content)); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	untyped := good
	untyped.MediaType = ""
	if stored, err := uc.Execute(ctx, node.ID(), execution, untyped, strings.NewReader(content)); err != nil || stored.MediaType != DefaultArtifactMediaType {
		t.Errorf("an artifact without a media type: %v %+v", err, stored)
	}
	h.store.listFail = errBoom
	if _, err := uc.Execute(ctx, node.ID(), execution, good, strings.NewReader(content)); !errors.Is(err, errBoom) {
		t.Errorf("list: %v", err)
	}
	h.store.listFail = nil
	for i := 0; i < domain.MaxArtifactsPerExecution-1; i++ {
		name := "f" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".txt"
		if _, err := uc.Execute(ctx, node.ID(), execution, delivered(name, content), strings.NewReader(content)); err != nil {
			t.Fatalf("artifact %d: %v", i, err)
		}
	}
	if _, err := uc.Execute(ctx, node.ID(), execution, delivered("one-too-many.txt", content), strings.NewReader(content)); err == nil {
		t.Error("a 65th artifact was kept")
	}
	if _, err := uc.Execute(ctx, node.ID(), execution, good, strings.NewReader(content)); err != nil {
		t.Errorf("replacing an artifact by name: %v", err)
	}
}

func TestTheControlPlaneKeepsWhatItRanItself(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task
	good := delivered("notes.md", "# notes\n")
	changed := delivered("plan.md", "# plan\n")
	missing := delivered("gone.md", "x")
	old := domain.Artifact{Name: "legacy.txt", MediaType: "text/plain", Reference: "legacy.txt"}
	h.work.files[bot.ID().String()+"/"+good.Reference] = "# notes\n"
	h.work.files[bot.ID().String()+"/"+changed.Reference] = "# plan, edited after delivery\n"
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", Artifacts: []domain.Artifact{good, changed, missing, old}}}
	res, err := NewRunTask(h.deps).Execute(ctx, task.ID())
	if err != nil {
		t.Fatal(err)
	}
	artifacts := res.Execution.Result().Artifacts
	if len(artifacts) != 5 || !artifacts[0].Stored || artifacts[1].Stored || artifacts[2].Stored || artifacts[3].Stored {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	failed := 0
	for _, a := range h.log.audits {
		if a.Action == "artifact.store" && a.Outcome == domain.AuditFailed {
			failed++
		}
	}
	if failed != 2 {
		t.Errorf("failed store audits = %d", failed)
	}
	if h.store.saveCalls != 2 {
		t.Errorf("save calls = %d: an artifact without a hash is listed, not read", h.store.saveCalls)
	}
}

func TestAnOversizedReceiptIsDroppedNotTheRun(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task
	h.runtime.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}, Receipt: json.RawMessage(`["not an object"]`)}
	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil || res.Task.Status() != domain.TaskCompleted || res.Execution.Receipt() != nil {
		t.Fatalf("run: %v %v", err, res.Execution.Receipt())
	}
	last := h.log.audits[len(h.log.audits)-1]
	if !strings.HasPrefix(last.Details["receipt"], "dropped: ") {
		t.Errorf("audit = %+v", last)
	}
}

func TestReportReferenceOfAnOlderExecution(t *testing.T) {
	execution, _ := domain.NewExecution("exec-1", "task-1", "bot-1", t0)
	_ = execution.Complete(domain.Result{Answer: "a", Artifacts: []domain.Artifact{{Name: ReportArtifactName, MediaType: ReportMediaType, Reference: "/state/artifacts/task-1/report.md"}}}, domain.Usage{}, "", t0)
	if ReportReference(execution) != "/state/artifacts/task-1/report.md" {
		t.Error("an execution recorded before sources lost its report")
	}
}
