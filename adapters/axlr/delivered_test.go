package axlr

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var _ application.Workspace = (*Executor)(nil)

func TestExecuteReadsTheDeliveriesAndTheReceipt(t *testing.T) {
	e, _ := fake(t, "result-delivered")
	report, err := e.Execute(context.Background(), request(time.Minute))
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("execute: %v %s", err, report.Outcome)
	}
	artifacts := report.Result.Artifacts
	want := domain.Artifact{Name: "notes.md", MediaType: "text/markdown", Size: 8, SHA256: strings.Repeat("ab", 32), Reference: ".axlr/deliverables/sess-1/notes.md",
		Description: "What I found", Source: "inline", SessionID: "sess-1", CallID: "deliver-1"}
	if len(artifacts) != 1 || artifacts[0] != want || !artifacts[0].Verifiable() {
		t.Errorf("artifacts = %+v", artifacts)
	}
	if string(report.Receipt) != `{"schema":"axlr.execution-receipt/v1","status":"completed","complete":true}` {
		t.Errorf("receipt = %s", report.Receipt)
	}
}

func TestAReceiptOfAnotherSchemaIsLeftInTheTrace(t *testing.T) {
	e, _ := fake(t, "receipt-unknown")
	report, err := e.Execute(context.Background(), request(time.Minute))
	if err != nil || report.Receipt != nil {
		t.Errorf("receipt = %s %v", report.Receipt, err)
	}
	p := newTurnParser()
	p.keepReceipt([]byte(`{"schema":"axlr.execution-receipt/v1","x":"` + strings.Repeat("a", domain.MaxReceiptBytes) + `"}`))
	if p.receipt != nil {
		t.Error("an oversized receipt was kept")
	}
	if p.interrupted("stopped").Receipt != nil {
		t.Error("an interrupted run without a receipt has one")
	}
}

func TestOpenDeliveredStaysInsideTheWorkspace(t *testing.T) {
	state := t.TempDir()
	e := NewExecutor("axlr-tui", state)
	workspace := filepath.Join(state, "workspaces", "bot")
	if err := os.MkdirAll(filepath.Join(workspace, ".axlr", "deliverables", "s1"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(workspace, ".axlr", "deliverables", "s1", "notes.md"), []byte("# notes\n"), 0o644)
	_ = os.WriteFile(filepath.Join(state, "secret.txt"), []byte("not yours"), 0o600)
	_ = os.Symlink(filepath.Join(state, "secret.txt"), filepath.Join(workspace, "link.txt"))
	_ = os.Symlink(filepath.Join(workspace, ".axlr", "deliverables", "s1", "notes.md"), filepath.Join(workspace, "inside.md"))
	_ = os.Mkdir(filepath.Join(workspace, "dir"), 0o755)
	big, _ := os.Create(filepath.Join(workspace, "big.bin"))
	_ = big.Truncate(domain.MaxArtifactBytes + 1)
	_ = big.Close()

	for _, good := range []string{".axlr/deliverables/s1/notes.md", "inside.md", "./inside.md", "dir/../inside.md", "dir//../inside.md"} {
		file, err := e.OpenDelivered("bot", good)
		if err != nil {
			t.Errorf("%s: %v", good, err)
			continue
		}
		data, _ := io.ReadAll(file)
		_ = file.Close()
		if string(data) != "# notes\n" {
			t.Errorf("%s = %q", good, data)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "../secret.txt", "..", ".", "dir/../../secret.txt", "link.txt", "dir", "big.bin", "missing.md", `a\b`, "a\x00b", "notes..md"} {
		if file, err := e.OpenDelivered("bot", bad); err == nil {
			_ = file.Close()
			t.Errorf("opened %q", bad)
		}
	}
	for _, bot := range []domain.BotID{"", "../bot", "a/b", "nobody"} {
		if _, err := e.OpenDelivered(bot, "inside.md"); err == nil {
			t.Errorf("opened the workspace of %q", bot)
		}
	}
}
